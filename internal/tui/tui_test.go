package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"thevault/internal/store"
)

// Drives the screen through walking and a fight, checking it renders.
func TestScreens(t *testing.T) {
	t.Setenv("THEVAULT_NOW", "2026-10-29T14:00:00-04:00")
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := New(db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if v := m.View(); !strings.Contains(v, "THE VAULT") || !strings.Contains(v, "alice") {
		t.Fatalf("map view missing parts:\n%s", v)
	}
	t.Log("\n" + m.View())

	// Put alice next to an enemy and walk into it.
	e := m.s.Enemies[0]
	if _, err := db.W().Exec(`UPDATE players SET room = ?, x = ?, y = ? WHERE user = 'alice'`, e.Room, e.X-1, e.Y); err != nil {
		t.Fatal(err)
	}
	m.refresh()
	m.key("right")
	if m.me.Battle != e.ID {
		t.Fatalf("expected a fight with enemy %d, msg %q", e.ID, m.msg)
	}
	m.fightFrom = m.fightFrom.Add(-time.Second)
	m.key("1")
	v := m.View()
	if !strings.Contains(v, "Attack") || !strings.Contains(v, e.Name) {
		t.Fatalf("battle view missing parts:\n%s", v)
	}
	t.Log("\n" + v)
}
