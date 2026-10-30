// Package store keeps the shared world in a SQLite file. Every command runs
// inside one BEGIN IMMEDIATE transaction, so two people logging in at once
// can never both run the daily tick.
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"

	"thevault/internal/world"
)

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS seasons (
	id INTEGER PRIMARY KEY, seed INTEGER NOT NULL, start_tick INTEGER NOT NULL,
	started_at INTEGER NOT NULL, won_tick INTEGER NOT NULL DEFAULT 0, winner TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS rooms (
	season INTEGER NOT NULL, num INTEGER NOT NULL, depth INTEGER NOT NULL, x INTEGER NOT NULL, y INTEGER NOT NULL,
	kind TEXT NOT NULL, revealed INTEGER NOT NULL, murk INTEGER NOT NULL, source INTEGER NOT NULL,
	safe_until INTEGER NOT NULL, box_open INTEGER NOT NULL, box_keys INTEGER NOT NULL,
	PRIMARY KEY (season, num));
CREATE TABLE IF NOT EXISTS doors (
	season INTEGER NOT NULL, a INTEGER NOT NULL, b INTEGER NOT NULL, kind TEXT NOT NULL,
	hidden INTEGER NOT NULL, revealed INTEGER NOT NULL, plate INTEGER NOT NULL,
	PRIMARY KEY (season, a, b));
CREATE TABLE IF NOT EXISTS items (
	season INTEGER NOT NULL, id INTEGER NOT NULL, kind TEXT NOT NULL, room INTEGER NOT NULL,
	holder TEXT NOT NULL, hidden INTEGER NOT NULL, in_box INTEGER NOT NULL,
	PRIMARY KEY (season, id));
CREATE TABLE IF NOT EXISTS players (
	user TEXT PRIMARY KEY, room INTEGER NOT NULL, hp INTEGER NOT NULL, turns INTEGER NOT NULL,
	downed INTEGER NOT NULL, joined_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS notes (
	id INTEGER PRIMARY KEY, season INTEGER NOT NULL, room INTEGER NOT NULL, user TEXT NOT NULL,
	text TEXT NOT NULL, at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY, tick INTEGER NOT NULL, at INTEGER NOT NULL, user TEXT NOT NULL,
	kind TEXT NOT NULL, text TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS events_tick ON events (tick);
`

type DB struct{ db *sql.DB }

func Open(path string) (*DB, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	d := &DB{db}
	if err := d.Tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(schema)
		return err
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	shareFile(path)
	return d, nil
}

// shareFile makes the database group-writable so every player can save.
// SQLite creates it 0644 whatever the umask says, and copies the main file's
// mode onto its -wal and -shm files, so fixing the main file is enough.
// Only the file's owner can chmod, which is whoever created it.
func shareFile(path string) {
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o020 == 0 {
		os.Chmod(path, fi.Mode().Perm()|0o060)
	}
}

func (d *DB) Close() error { return d.db.Close() }

// Tx runs fn in a write-locked transaction, committing if it returns nil.
func (d *DB) Tx(fn func(*sql.Tx) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func getMeta(tx *sql.Tx, key string) (string, error) {
	var v string
	err := tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func setMeta(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Load reads the current season and all players. A brand new database
// returns a state with Season.ID == 0.
func Load(tx *sql.Tx, now time.Time) (*world.State, error) {
	s := &world.State{Now: now, Players: map[string]*world.Player{}}

	tick, err := getMeta(tx, "tick")
	if err != nil {
		return nil, err
	}
	if tick == "" {
		return s, nil
	}
	s.Tick, _ = strconv.Atoi(tick)
	last, err := getMeta(tx, "last_tick")
	if err != nil {
		return nil, err
	}
	sec, _ := strconv.ParseInt(last, 10, 64)
	s.LastTick = time.Unix(sec, 0)

	var started int64
	var seed int64
	err = tx.QueryRow(`SELECT id, seed, start_tick, started_at, won_tick, winner FROM seasons ORDER BY id DESC LIMIT 1`).
		Scan(&s.Season.ID, &seed, &s.Season.StartTick, &started, &s.Season.WonTick, &s.Season.Winner)
	if err != nil {
		return nil, fmt.Errorf("load season: %w", err)
	}
	s.Season.Seed = uint64(seed)
	s.Season.StartedAt = time.Unix(started, 0)
	sid := s.Season.ID

	rows, err := tx.Query(`SELECT num, depth, x, y, kind, revealed, murk, source, safe_until, box_open, box_keys
		FROM rooms WHERE season = ? ORDER BY num`, sid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		r := &world.Room{}
		if err := rows.Scan(&r.Num, &r.Depth, &r.X, &r.Y, &r.Kind, &r.Revealed, &r.Murk, &r.Source, &r.SafeUntil, &r.BoxOpen, &r.BoxKeys); err != nil {
			rows.Close()
			return nil, err
		}
		s.Rooms = append(s.Rooms, r)
	}
	rows.Close()

	rows, err = tx.Query(`SELECT a, b, kind, hidden, revealed, plate FROM doors WHERE season = ?`, sid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		d := &world.Door{}
		if err := rows.Scan(&d.A, &d.B, &d.Kind, &d.Hidden, &d.Revealed, &d.Plate); err != nil {
			rows.Close()
			return nil, err
		}
		s.Doors = append(s.Doors, d)
	}
	rows.Close()

	rows, err = tx.Query(`SELECT id, kind, room, holder, hidden, in_box FROM items WHERE season = ? ORDER BY id`, sid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		it := &world.Item{}
		if err := rows.Scan(&it.ID, &it.Kind, &it.Room, &it.Holder, &it.Hidden, &it.InBox); err != nil {
			rows.Close()
			return nil, err
		}
		s.Items = append(s.Items, it)
	}
	rows.Close()

	rows, err = tx.Query(`SELECT user, room, hp, turns, downed, joined_at FROM players`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p := &world.Player{}
		var joined int64
		if err := rows.Scan(&p.User, &p.Room, &p.HP, &p.Turns, &p.Downed, &joined); err != nil {
			return nil, err
		}
		p.Joined = time.Unix(joined, 0)
		s.Players[p.User] = p
	}
	return s, rows.Err()
}

// Save writes the state back. The world is tiny (about 70 rooms), so it
// simply rewrites every row of the current season.
func Save(tx *sql.Tx, s *world.State) error {
	sid := s.Season.ID
	if s.FreshWorld {
		if _, err := tx.Exec(`INSERT INTO seasons (id, seed, start_tick, started_at) VALUES (?, ?, ?, ?)`,
			sid, int64(s.Season.Seed), s.Season.StartTick, s.Season.StartedAt.Unix()); err != nil {
			return err
		}
		for _, r := range s.Rooms {
			if _, err := tx.Exec(`INSERT INTO rooms (season, num, depth, x, y, kind, revealed, murk, source, safe_until, box_open, box_keys)
				VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0)`, sid, r.Num, r.Depth, r.X, r.Y, r.Kind); err != nil {
				return err
			}
		}
		for _, d := range s.Doors {
			if _, err := tx.Exec(`INSERT INTO doors (season, a, b, kind, hidden, revealed, plate) VALUES (?, ?, ?, '', 0, 0, ?)`,
				sid, d.A, d.B, d.Plate); err != nil {
				return err
			}
		}
		for _, it := range s.Items {
			if _, err := tx.Exec(`INSERT INTO items (season, id, kind, room, holder, hidden, in_box) VALUES (?, ?, ?, 0, '', 0, 0)`,
				sid, it.ID, it.Kind); err != nil {
				return err
			}
		}
		s.FreshWorld = false
	}

	if _, err := tx.Exec(`UPDATE seasons SET won_tick = ?, winner = ? WHERE id = ?`, s.Season.WonTick, s.Season.Winner, sid); err != nil {
		return err
	}
	roomStmt, err := tx.Prepare(`UPDATE rooms SET revealed = ?, murk = ?, source = ?, safe_until = ?, box_open = ?, box_keys = ? WHERE season = ? AND num = ?`)
	if err != nil {
		return err
	}
	defer roomStmt.Close()
	for _, r := range s.Rooms {
		if _, err := roomStmt.Exec(b2i(r.Revealed), b2i(r.Murk), b2i(r.Source), r.SafeUntil, b2i(r.BoxOpen), r.BoxKeys, sid, r.Num); err != nil {
			return err
		}
	}
	doorStmt, err := tx.Prepare(`UPDATE doors SET kind = ?, hidden = ?, revealed = ? WHERE season = ? AND a = ? AND b = ?`)
	if err != nil {
		return err
	}
	defer doorStmt.Close()
	for _, d := range s.Doors {
		if _, err := doorStmt.Exec(d.Kind, b2i(d.Hidden), b2i(d.Revealed), sid, d.A, d.B); err != nil {
			return err
		}
	}
	itemStmt, err := tx.Prepare(`UPDATE items SET room = ?, holder = ?, hidden = ?, in_box = ? WHERE season = ? AND id = ?`)
	if err != nil {
		return err
	}
	defer itemStmt.Close()
	for _, it := range s.Items {
		if _, err := itemStmt.Exec(it.Room, it.Holder, b2i(it.Hidden), b2i(it.InBox), sid, it.ID); err != nil {
			return err
		}
	}
	for _, p := range s.Players {
		if _, err := tx.Exec(`INSERT INTO players (user, room, hp, turns, downed, joined_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (user) DO UPDATE SET room = excluded.room, hp = excluded.hp, turns = excluded.turns, downed = excluded.downed`,
			p.User, p.Room, p.HP, p.Turns, b2i(p.Downed), p.Joined.Unix()); err != nil {
			return err
		}
	}
	for _, n := range s.Notes {
		if _, err := tx.Exec(`INSERT INTO notes (season, room, user, text, at) VALUES (?, ?, ?, ?, ?)`,
			sid, n.Room, n.User, n.Text, n.At.Unix()); err != nil {
			return err
		}
	}
	s.Notes = nil
	for _, e := range s.Events {
		if _, err := tx.Exec(`INSERT INTO events (tick, at, user, kind, text) VALUES (?, ?, ?, ?, ?)`,
			e.Tick, e.At.Unix(), e.User, e.Kind, e.Text); err != nil {
			return err
		}
	}
	s.Events = nil
	if err := setMeta(tx, "tick", strconv.Itoa(s.Tick)); err != nil {
		return err
	}
	return setMeta(tx, "last_tick", strconv.FormatInt(s.LastTick.Unix(), 10))
}

// Notes returns the newest notes in a room, newest first.
func Notes(tx *sql.Tx, season, room, limit int) ([]world.Note, error) {
	rows, err := tx.Query(`SELECT room, user, text, at FROM notes WHERE season = ? AND room = ? ORDER BY id DESC LIMIT ?`, season, room, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []world.Note
	for rows.Next() {
		var n world.Note
		var at int64
		if err := rows.Scan(&n.Room, &n.User, &n.Text, &at); err != nil {
			return nil, err
		}
		n.At = time.Unix(at, 0)
		out = append(out, n)
	}
	return out, rows.Err()
}

// Events returns events from sinceTick onward, newest first.
func Events(tx *sql.Tx, sinceTick, limit int) ([]world.Event, error) {
	rows, err := tx.Query(`SELECT tick, at, user, kind, text FROM events WHERE tick >= ? ORDER BY id DESC LIMIT ?`, sinceTick, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []world.Event
	for rows.Next() {
		var e world.Event
		var at int64
		if err := rows.Scan(&e.Tick, &at, &e.User, &e.Kind, &e.Text); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
