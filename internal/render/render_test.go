package render

import (
	"strings"
	"testing"

	"thevault/internal/world"
)

func TestEnemyTileHidesLevel(t *testing.T) {
	e := &world.Enemy{Name: "Typosquatter", Level: 3}
	if got := EnemyTile(e); !strings.Contains(got, "t") || strings.Contains(got, "3") {
		t.Fatalf("map tile should be the icon only, got %q", got)
	}
}
