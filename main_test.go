package main

import (
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"thevault/internal/store"
	"thevault/internal/world"
)

// Ten people logging in at once after 3 AM must produce exactly one tick.
func TestConcurrentLoginsTickOnce(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	t.Setenv("THEVAULT_NOW", "2026-10-29T14:00:00-04:00")
	if err := run(db, "alice", nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THEVAULT_NOW", "2026-10-30T09:00:00-04:00")
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := run(db, fmt.Sprintf("user%d", i), []string{"motd"}, io.Discard); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	d, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Tx(func(tx *sql.Tx) error {
		s, err := store.Load(tx, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if s.Tick != 2 {
			t.Fatalf("tick = %d, want 2", s.Tick)
		}
		if len(s.Players) != 11 {
			t.Fatalf("players = %d, want 11", len(s.Players))
		}
		if p := s.Players["alice"]; p.Energy != 2*world.EnergyPerTick {
			t.Fatalf("alice energy = %d, want %d (start + one refill)", p.Energy, 2*world.EnergyPerTick)
		}
		return nil
	})
}
