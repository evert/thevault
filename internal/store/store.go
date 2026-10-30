// Package store keeps the shared world in a SQLite file.
//
// Every action runs in one BEGIN IMMEDIATE transaction (load, change, save),
// so two people acting at once never overwrite each other and the daily tick
// can't run twice. The live screen refreshes through a separate read-only
// connection, which WAL mode lets run alongside writers.
package store

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"thevault/internal/world"
)

// schemaVersion is bumped whenever the tables change. Versions before 2
// predate the current game and are wiped; later ones are migrated.
const schemaVersion = 3

const schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE seasons (
	id INTEGER PRIMARY KEY, seed INTEGER NOT NULL, start_tick INTEGER NOT NULL,
	started_at INTEGER NOT NULL, won_tick INTEGER NOT NULL DEFAULT 0, winner TEXT NOT NULL DEFAULT '');
CREATE TABLE rooms (season INTEGER NOT NULL, num INTEGER NOT NULL, revealed INTEGER NOT NULL, PRIMARY KEY (season, num));
CREATE TABLE enemies (
	season INTEGER NOT NULL, id INTEGER NOT NULL, room INTEGER NOT NULL, x INTEGER NOT NULL, y INTEGER NOT NULL,
	name TEXT NOT NULL, level INTEGER NOT NULL, hp INTEGER NOT NULL, max_hp INTEGER NOT NULL, atk INTEGER NOT NULL,
	boss INTEGER NOT NULL, hitters TEXT NOT NULL, PRIMARY KEY (season, id));
CREATE TABLE items (
	season INTEGER NOT NULL, id INTEGER NOT NULL, kind TEXT NOT NULL, room INTEGER NOT NULL,
	x INTEGER NOT NULL, y INTEGER NOT NULL, holder TEXT NOT NULL, PRIMARY KEY (season, id));
CREATE TABLE players (
	user TEXT PRIMARY KEY, color INTEGER NOT NULL, room INTEGER NOT NULL, x INTEGER NOT NULL, y INTEGER NOT NULL,
	hp INTEGER NOT NULL, level INTEGER NOT NULL, xp INTEGER NOT NULL, energy INTEGER NOT NULL,
	battle INTEGER NOT NULL, braced INTEGER NOT NULL, ko INTEGER NOT NULL,
	last_seen INTEGER NOT NULL, joined_at INTEGER NOT NULL, known TEXT NOT NULL DEFAULT '');
CREATE TABLE events (
	id INTEGER PRIMARY KEY, season INTEGER NOT NULL, tick INTEGER NOT NULL, at INTEGER NOT NULL, user TEXT NOT NULL,
	kind TEXT NOT NULL, text TEXT NOT NULL, enemy INTEGER NOT NULL DEFAULT 0);
CREATE INDEX events_tick ON events (tick);
CREATE INDEX events_enemy ON events (season, enemy);
`

var tables = []string{"meta", "seasons", "rooms", "doors", "enemies", "items", "players", "notes", "events"}

type DB struct{ w, r *sql.DB }

func Open(path string) (*DB, error) {
	dsn := func(lock string) string {
		q := url.Values{}
		q.Add("_pragma", "busy_timeout(10000)")
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
		q.Set("_txlock", lock)
		return "file:" + path + "?" + q.Encode()
	}
	w, err := sql.Open("sqlite", dsn("immediate"))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", dsn("deferred"))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(1)
	d := &DB{w: w, r: r}
	if err := d.Tx(migrate); err != nil {
		d.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	shareFile(path)
	return d, nil
}

func migrate(tx *sql.Tx) error {
	var v int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v == schemaVersion {
		return nil
	}
	if v == 2 {
		if _, err := tx.Exec(`ALTER TABLE players ADD COLUMN known TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		_, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
		return err
	}
	for _, t := range tables {
		if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	_, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
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

func (d *DB) Close() error {
	d.r.Close()
	return d.w.Close()
}

// Tx runs fn in a write-locked transaction, committing if it returns nil.
func (d *DB) Tx(fn func(*sql.Tx) error) error { return run(d.w, fn) }

// Read runs fn in a read-only snapshot. Changes are rolled back.
func (d *DB) Read(fn func(*sql.Tx) error) error {
	tx, err := d.r.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(tx)
}

func run(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
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
	s := &world.State{
		Now:     now,
		Players: map[string]*world.Player{},
		Rng:     rand.New(rand.NewPCG(uint64(now.UnixNano()), uint64(os.Getpid()))),
	}
	tick, err := getMeta(tx, "tick")
	if err != nil || tick == "" {
		return s, err
	}
	s.Tick, _ = strconv.Atoi(tick)
	last, err := getMeta(tx, "last_tick")
	if err != nil {
		return nil, err
	}
	sec, _ := strconv.ParseInt(last, 10, 64)
	s.LastTick = time.Unix(sec, 0)

	var seed, started int64
	err = tx.QueryRow(`SELECT id, seed, start_tick, started_at, won_tick, winner FROM seasons ORDER BY id DESC LIMIT 1`).
		Scan(&s.Season.ID, &seed, &s.Season.StartTick, &started, &s.Season.WonTick, &s.Season.Winner)
	if err != nil {
		return nil, fmt.Errorf("load season: %w", err)
	}
	s.Season.Seed = uint64(seed)
	s.Season.StartedAt = time.Unix(started, 0)
	sid := s.Season.ID
	seq, err := getMeta(tx, "enemy_seq")
	if err != nil {
		return nil, err
	}
	s.EnemySeq, _ = strconv.Atoi(seq)

	// The dungeon's shape comes from the seed; only what changes is stored.
	s.Rooms, s.Doors, _, _ = world.Generate(s.Season.Seed)

	if err := each(tx, `SELECT num, revealed FROM rooms WHERE season = ?`, []any{sid}, func(rows *sql.Rows) error {
		var num int
		var rev bool
		if err := rows.Scan(&num, &rev); err != nil {
			return err
		}
		if rm := s.Room(num); rm != nil {
			rm.Revealed = rev
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each(tx, `SELECT id, room, x, y, name, level, hp, max_hp, atk, boss, hitters FROM enemies WHERE season = ? ORDER BY id`, []any{sid}, func(rows *sql.Rows) error {
		e := &world.Enemy{Hitters: map[string]bool{}}
		var hitters string
		if err := rows.Scan(&e.ID, &e.Room, &e.X, &e.Y, &e.Name, &e.Level, &e.HP, &e.MaxHP, &e.Atk, &e.Boss, &hitters); err != nil {
			return err
		}
		for _, u := range splitNames(hitters) {
			e.Hitters[u] = true
		}
		s.Enemies = append(s.Enemies, e)
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each(tx, `SELECT id, kind, room, x, y, holder FROM items WHERE season = ? ORDER BY id`, []any{sid}, func(rows *sql.Rows) error {
		it := &world.Item{}
		if err := rows.Scan(&it.ID, &it.Kind, &it.Room, &it.X, &it.Y, &it.Holder); err != nil {
			return err
		}
		s.Items = append(s.Items, it)
		return nil
	}); err != nil {
		return nil, err
	}

	err = each(tx, `SELECT user, color, room, x, y, hp, level, xp, energy, battle, braced, ko, last_seen, joined_at, known FROM players`, nil, func(rows *sql.Rows) error {
		p := &world.Player{Known: map[string]bool{}}
		var seen, joined int64
		var known string
		if err := rows.Scan(&p.User, &p.Color, &p.Room, &p.X, &p.Y, &p.HP, &p.Level, &p.XP, &p.Energy,
			&p.Battle, &p.Braced, &p.KO, &seen, &joined, &known); err != nil {
			return err
		}
		for _, name := range splitNames(known) {
			p.Known[name] = true
		}
		p.LastSeen, p.Joined = time.Unix(seen, 0), time.Unix(joined, 0)
		s.Players[p.User] = p
		return nil
	})
	return s, err
}

func each(tx *sql.Tx, query string, args []any, fn func(*sql.Rows) error) error {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// joinSet stores a set of names as a sorted, comma-separated string.
func joinSet(set map[string]bool) string {
	var names []string
	for name, ok := range set {
		if ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func splitNames(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// Save writes the state back. The world is small (about 50 rooms, a few
// dozen enemies and packages), so it simply rewrites the season's rows.
func Save(tx *sql.Tx, s *world.State) error {
	sid := s.Season.ID
	if s.FreshWorld {
		if _, err := tx.Exec(`INSERT INTO seasons (id, seed, start_tick, started_at) VALUES (?, ?, ?, ?)`,
			sid, int64(s.Season.Seed), s.Season.StartTick, s.Season.StartedAt.Unix()); err != nil {
			return err
		}
		s.FreshWorld = false
	}
	if _, err := tx.Exec(`UPDATE seasons SET won_tick = ?, winner = ? WHERE id = ?`, s.Season.WonTick, s.Season.Winner, sid); err != nil {
		return err
	}
	for _, t := range []string{"rooms", "enemies", "items"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE season = ?`, sid); err != nil {
			return err
		}
	}
	if err := insertAll(tx, `INSERT INTO rooms (season, num, revealed) VALUES (?, ?, ?)`, len(s.Rooms), func(i int) []any {
		return []any{sid, s.Rooms[i].Num, b2i(s.Rooms[i].Revealed)}
	}); err != nil {
		return err
	}
	if err := insertAll(tx, `INSERT INTO enemies (season, id, room, x, y, name, level, hp, max_hp, atk, boss, hitters)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, len(s.Enemies), func(i int) []any {
		e := s.Enemies[i]
		return []any{sid, e.ID, e.Room, e.X, e.Y, e.Name, e.Level, e.HP, e.MaxHP, e.Atk, b2i(e.Boss), joinSet(e.Hitters)}
	}); err != nil {
		return err
	}
	if err := insertAll(tx, `INSERT INTO items (season, id, kind, room, x, y, holder) VALUES (?, ?, ?, ?, ?, ?, ?)`, len(s.Items), func(i int) []any {
		it := s.Items[i]
		return []any{sid, it.ID, it.Kind, it.Room, it.X, it.Y, it.Holder}
	}); err != nil {
		return err
	}
	users := s.Users()
	if err := insertAll(tx, `INSERT INTO players (user, color, room, x, y, hp, level, xp, energy, battle, braced, ko, last_seen, joined_at, known)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user) DO UPDATE SET color = excluded.color, room = excluded.room, x = excluded.x, y = excluded.y,
			hp = excluded.hp, level = excluded.level, xp = excluded.xp, energy = excluded.energy, battle = excluded.battle,
			braced = excluded.braced, ko = excluded.ko, last_seen = excluded.last_seen, known = excluded.known`, len(users), func(i int) []any {
		p := s.Players[users[i]]
		return []any{p.User, p.Color, p.Room, p.X, p.Y, p.HP, p.Level, p.XP, p.Energy, p.Battle, b2i(p.Braced), b2i(p.KO),
			p.LastSeen.Unix(), p.Joined.Unix(), joinSet(p.Known)}
	}); err != nil {
		return err
	}
	if err := insertAll(tx, `INSERT INTO events (season, tick, at, user, kind, text, enemy) VALUES (?, ?, ?, ?, ?, ?, ?)`, len(s.Events), func(i int) []any {
		e := s.Events[i]
		return []any{sid, e.Tick, e.At.Unix(), e.User, e.Kind, e.Text, e.Enemy}
	}); err != nil {
		return err
	}
	s.Events = nil
	if err := setMeta(tx, "tick", strconv.Itoa(s.Tick)); err != nil {
		return err
	}
	if err := setMeta(tx, "enemy_seq", strconv.Itoa(s.EnemySeq)); err != nil {
		return err
	}
	return setMeta(tx, "last_tick", strconv.FormatInt(s.LastTick.Unix(), 10))
}

func insertAll(tx *sql.Tx, query string, n int, row func(int) []any) error {
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := range n {
		if _, err := stmt.Exec(row(i)...); err != nil {
			return err
		}
	}
	return nil
}

// Events returns world events (not battle lines) from sinceTick on, newest first.
func Events(tx *sql.Tx, sinceTick, limit int) ([]world.Event, error) {
	return events(tx, `SELECT tick, at, user, kind, text, enemy FROM events WHERE tick >= ? AND enemy = 0 ORDER BY id DESC LIMIT ?`, sinceTick, limit)
}

// BattleLog returns the newest lines of one enemy's fight, newest first.
func BattleLog(tx *sql.Tx, season, enemy, limit int) ([]world.Event, error) {
	return events(tx, `SELECT tick, at, user, kind, text, enemy FROM events WHERE season = ? AND enemy = ? ORDER BY id DESC LIMIT ?`, season, enemy, limit)
}

func events(tx *sql.Tx, query string, args ...any) ([]world.Event, error) {
	var out []world.Event
	err := each(tx, query, args, func(rows *sql.Rows) error {
		var e world.Event
		var at int64
		if err := rows.Scan(&e.Tick, &at, &e.User, &e.Kind, &e.Text, &e.Enemy); err != nil {
			return err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
		return nil
	})
	return out, err
}

// W exposes the write connection, for tests that need to arrange state.
func (d *DB) W() *sql.DB { return d.w }
