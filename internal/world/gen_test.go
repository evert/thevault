package world

import (
	"reflect"
	"testing"
)

func TestGenerateDeterministic(t *testing.T) {
	r1, d1, i1 := Generate(42)
	r2, d2, i2 := Generate(42)
	if !reflect.DeepEqual(r1, r2) || !reflect.DeepEqual(d1, d2) || !reflect.DeepEqual(i1, i2) {
		t.Fatal("same seed gave different dungeons")
	}
	r3, _, _ := Generate(43)
	if reflect.DeepEqual(r1, r3) {
		t.Fatal("different seeds gave the same dungeon")
	}
}

func TestGenerateSolvable(t *testing.T) {
	for seed := uint64(0); seed < 500; seed++ {
		rooms, doors, items := Generate(seed)
		s := &State{Rooms: rooms, Doors: doors, Items: items}
		for depth := 1; depth <= Depths; depth++ {
			ent, st, box := s.FindKind(depth, Entrance), s.FindKind(depth, Stairs), s.FindKind(depth, Tarball)
			if ent == nil || st == nil || box == nil {
				t.Fatalf("seed %d depth %d: missing entrance/stairs/tarball", seed, depth)
			}
			n := len(s.DepthRooms(depth))
			if n < 10 || n > 14 {
				t.Fatalf("seed %d depth %d: %d rooms", seed, depth, n)
			}
			// Open doors alone, or open doors plus a key found on the way,
			// must lead to the stairs. Plates are never required.
			open := reach(s, ent.Num, false)
			if !open[st.Num] {
				hasKey := false
				for _, it := range items {
					if it.Kind == Key && open[it.Room] && !it.InBox {
						hasKey = true
					}
				}
				if !hasKey || !reach(s, ent.Num, true)[st.Num] {
					t.Fatalf("seed %d depth %d: stairs unreachable", seed, depth)
				}
			}
			if ent.Murk || st.Murk || box.Murk {
				t.Fatalf("seed %d depth %d: murk starts in a key room", seed, depth)
			}
		}
	}
}

func reach(s *State, from int, unlock bool) map[int]bool {
	seen := map[int]bool{from: true}
	queue := []int{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range s.DoorsOf(cur) {
			if d.Kind == Plate || (d.Kind == Locked && !unlock) {
				continue
			}
			if o := d.Other(cur); !seen[o] {
				seen[o] = true
				queue = append(queue, o)
			}
		}
	}
	return seen
}
