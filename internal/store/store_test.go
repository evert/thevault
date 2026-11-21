package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"thevault/internal/game"
	"thevault/internal/store"
	"thevault/internal/world"
)

var now = time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC)

func newWorld(t *testing.T) (string, *store.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Tx(func(tx *sql.Tx) error {
		s, err := store.Load(tx, now)
		if err != nil {
			return err
		}
		game.Start(s, 7)
		game.Join(s, "alice")
		return store.Save(tx, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	return path, db
}

func load(t *testing.T, db *store.DB) *world.State {
	t.Helper()
	var s *world.State
	if err := db.Read(func(tx *sql.Tx) (err error) { s, err = store.Load(tx, now); return }); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLocksAndTokensSurviveSaving(t *testing.T) {
	_, db := newWorld(t)
	defer db.Close()
	s := load(t, db)
	if !s.FindKind(1, world.Stairs).Locked() {
		t.Fatal("floor 1 stairs aren't locked after a reload")
	}
	_, _, enemies, items := world.Generate(7)
	want := 0
	for _, e := range enemies {
		if e.Key {
			want++
		}
	}
	for _, it := range items {
		if world.IsToken(it.Kind) {
			want++
		}
	}
	got := 0
	for _, e := range s.Enemies {
		if e.Key {
			got++
		}
	}
	for _, it := range s.Items {
		if world.IsToken(it.Kind) {
			got++
		}
	}
	if got != want || got != world.Floors-1 {
		t.Fatalf("tokens after reload: got %d, want %d", got, want)
	}
}

// A world saved before tokens existed has none to find, so it must stay open.
func TestOlderWorldsStayUnlocked(t *testing.T) {
	path, db := newWorld(t)
	for _, q := range []string{
		`ALTER TABLE rooms DROP COLUMN unlocked`,
		`ALTER TABLE rooms DROP COLUMN visited`,
		`ALTER TABLE enemies DROP COLUMN key`,
		`PRAGMA user_version = 3`,
	} {
		if _, err := db.W().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, rm := range load(t, db).Rooms {
		if rm.Locked() {
			t.Fatalf("room %d is locked in a world from before tokens", rm.Num)
		}
	}
}

// A world saved before rooms tracked visits upgrades in place, keeping its
// enemies, and comes back as needing a rebalance.
func TestWorldsFromBeforeRespawnUpgrade(t *testing.T) {
	path, db := newWorld(t)
	before := len(load(t, db).Enemies)
	for _, q := range []string{
		`ALTER TABLE rooms DROP COLUMN visited`,
		`DELETE FROM meta WHERE key = 'balance'`,
		`PRAGMA user_version = 4`,
	} {
		if _, err := db.W().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := load(t, db)
	if len(s.Enemies) != before || s.Balance != 0 {
		t.Fatalf("after upgrade: %d enemies (want %d), balance %d", len(s.Enemies), before, s.Balance)
	}
	for _, rm := range s.Rooms {
		if !rm.Visited.IsZero() {
			t.Fatalf("room %d has a visit time it never had", rm.Num)
		}
	}
}
