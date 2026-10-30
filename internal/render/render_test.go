package render

import (
	"strings"
	"testing"

	"thevault/internal/world"
)

func TestLevelsShowOnlyForKnownTypes(t *testing.T) {
	e := &world.Enemy{Name: "Typosquatter", Level: 3}
	alice := &world.Player{User: "alice", Known: map[string]bool{"Typosquatter": true}}
	bob := &world.Player{User: "bob", Known: map[string]bool{}}
	if got := EnemyTile(e, alice); !strings.Contains(got, "t") || !strings.Contains(got, "3") {
		t.Fatalf("alice knows Typosquatters but sees %q", got)
	}
	if got := EnemyTile(e, bob); strings.Contains(got, "3") {
		t.Fatalf("bob has never fought one but sees its level: %q", got)
	}
}
