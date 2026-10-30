package world

import (
	"reflect"
	"strings"
	"testing"
)

func TestGenerateDeterministic(t *testing.T) {
	r1, d1, e1, i1 := Generate(42)
	r2, d2, e2, i2 := Generate(42)
	if !reflect.DeepEqual(r1, r2) || !reflect.DeepEqual(d1, d2) || !reflect.DeepEqual(e1, e2) || !reflect.DeepEqual(i1, i2) {
		t.Fatal("same seed gave different dungeons")
	}
	r3, _, _, _ := Generate(43)
	if reflect.DeepEqual(r1, r3) {
		t.Fatal("different seeds gave the same dungeon")
	}
}

func TestGenerateShape(t *testing.T) {
	for seed := uint64(0); seed < 300; seed++ {
		rooms, doors, enemies, items := Generate(seed)
		s := &State{Rooms: rooms, Doors: doors, Enemies: enemies, Items: items}
		bosses := 0
		for _, e := range enemies {
			if e.Boss {
				bosses++
			}
			if s.Room(e.Room).At(e.X, e.Y) != Floor {
				t.Fatalf("seed %d: enemy %d not on a floor tile", seed, e.ID)
			}
		}
		if bosses != 1 {
			t.Fatalf("seed %d: %d bosses", seed, bosses)
		}
		for floor := 1; floor <= Floors; floor++ {
			rs := s.FloorRooms(floor)
			if len(rs) < 8 || len(rs) > 11 {
				t.Fatalf("seed %d floor %d: %d rooms", seed, floor, len(rs))
			}
			if s.FindKind(floor, Stairs) == nil || (floor > 1) != (s.FindKind(floor, Entrance) != nil) {
				t.Fatalf("seed %d floor %d: missing stairs or entrance", seed, floor)
			}
			// Every room on the floor is reachable through doors.
			seen := map[int]bool{rs[0].Num: true}
			queue := []int{rs[0].Num}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				for _, d := range doors {
					if (d.A == cur || d.B == cur) && !seen[d.Other(cur)] {
						seen[d.Other(cur)] = true
						queue = append(queue, d.Other(cur))
					}
				}
			}
			if len(seen) != len(rs) {
				t.Fatalf("seed %d floor %d: only %d/%d rooms connected", seed, floor, len(seen), len(rs))
			}
		}
		for _, rm := range rooms {
			checkWalkable(t, seed, rm)
		}
	}
}

// Every door and staircase in a room must be reachable on foot from the middle.
func checkWalkable(t *testing.T, seed uint64, rm *Room) {
	seen := map[[2]int]bool{}
	stack := [][2]int{{RoomW / 2, RoomH / 2}}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[c] || rm.At(c[0], c[1]) == Wall {
			continue
		}
		seen[c] = true
		for _, d := range Dirs {
			stack = append(stack, [2]int{c[0] + d[0], c[1] + d[1]})
		}
	}
	for y := range RoomH {
		for x := range RoomW {
			if tile := rm.Tiles[y][x]; tile != Wall && !seen[[2]int{x, y}] {
				t.Fatalf("seed %d room %d: tile %c at %d,%d unreachable", seed, rm.Num, tile, x, y)
			}
		}
	}
	if rm.Kind == Stairs && rm.Floor < Floors {
		if x, _ := rm.Find(StairDown); x < 0 {
			t.Fatalf("seed %d room %d: no stairs down", seed, rm.Num)
		}
	}
	if rm.Kind == Entrance {
		if x, _ := rm.Find(StairUp); x < 0 {
			t.Fatalf("seed %d room %d: no stairs up", seed, rm.Num)
		}
	}
}

func TestEnemyTypesLookDifferent(t *testing.T) {
	glyphs := map[string]string{Boss.Glyph: Boss.Name}
	for _, tier := range Tiers {
		for _, et := range tier {
			if other, dup := glyphs[et.Glyph]; dup {
				t.Fatalf("%s and %s share the icon %q", et.Name, other, et.Glyph)
			}
			if et.Glyph != strings.ToLower(et.Glyph) {
				t.Fatalf("%s uses an uppercase icon; those are for players", et.Name)
			}
			glyphs[et.Glyph] = et.Name
		}
	}
}
