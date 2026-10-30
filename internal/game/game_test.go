package game

import (
	"strconv"
	"testing"
	"time"

	"thevault/internal/world"
)

var t0 = time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)

func newState(seed uint64) *world.State {
	s := &world.State{Now: t0, Players: map[string]*world.Player{}}
	Start(s, seed)
	return s
}

func seq() func() uint64 {
	n := uint64(1000)
	return func() uint64 { n++; return n }
}

func TestTickRefillsAndCaps(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	p.Turns = 2
	Tick(s, seq())
	if p.Turns != 5 {
		t.Fatalf("turns = %d, want 5", p.Turns)
	}
	Tick(s, seq())
	if p.Turns != world.MaxTurns {
		t.Fatalf("turns = %d, want cap %d", p.Turns, world.MaxTurns)
	}
}

func TestCatchUpRunsOncePerBoundary(t *testing.T) {
	s := newState(1)
	Join(s, "alice")
	s.Now = s.Now.Add(3 * 24 * time.Hour)
	if n := CatchUp(s, seq()); n != 3 {
		t.Fatalf("ran %d ticks, want 3", n)
	}
	if n := CatchUp(s, seq()); n != 0 {
		t.Fatalf("second catch-up ran %d ticks, want 0", n)
	}
	if s.Tick != 4 {
		t.Fatalf("tick = %d, want 4", s.Tick)
	}
}

func TestMurkHurtsDownsAndRescues(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	var murky *world.Room
	for _, r := range s.DepthRooms(1) {
		if r.Murk {
			murky = r
		}
	}
	p.Room = murky.Num
	Tick(s, seq())
	if p.HP != world.MaxHP-1 {
		t.Fatalf("hp = %d after one night in murk", p.HP)
	}
	p.HP = 1
	Tick(s, seq())
	if !p.Downed || p.HP != 0 {
		t.Fatalf("expected downed, got hp=%d downed=%v", p.HP, p.Downed)
	}
	if _, err := Search(s, p); !IsOops(err) {
		t.Fatalf("downed player could act: %v", err)
	}
	Tick(s, seq())
	if p.Downed || p.HP != world.MaxHP || p.Room != s.Camp().Num {
		t.Fatalf("not rescued to camp: %+v", p)
	}
}

func TestRopeSavesOnce(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	for _, r := range s.DepthRooms(1) {
		if r.Murk {
			p.Room = r.Num
		}
	}
	for _, it := range s.Items {
		if it.Kind == world.Rope {
			it.Holder, it.Room, it.Hidden = "alice", 0, false
			break
		}
	}
	Tick(s, seq())
	if p.HP != world.MaxHP || s.Holding("alice", world.Rope) != nil {
		t.Fatalf("rope should absorb one night: hp=%d", p.HP)
	}
}

func TestMurkOnlySpreadsOnReachedDepths(t *testing.T) {
	s := newState(1)
	before := murkCount(s, 3)
	for range 5 {
		Tick(s, seq())
	}
	if murkCount(s, 3) != before {
		t.Fatal("murk spread on an unreached depth")
	}
	if murkCount(s, 1) <= 1 {
		t.Fatal("murk did not spread on depth 1")
	}
}

func murkCount(s *world.State, depth int) int {
	n := 0
	for _, r := range s.DepthRooms(depth) {
		if r.Murk {
			n++
		}
	}
	return n
}

func TestOutOfTurns(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	p.Turns = 0
	if _, err := Search(s, p); !IsOops(err) {
		t.Fatalf("expected out-of-turns, got %v", err)
	}
}

func TestPlateDoorNeedsSomeoneElse(t *testing.T) {
	for seed := uint64(0); seed < 200; seed++ {
		s := newState(seed)
		var door *world.Door
		for _, d := range s.Doors {
			if d.Kind == world.Plate {
				door = d
				break
			}
		}
		if door == nil {
			continue
		}
		door.Revealed = true
		a, b := Join(s, "alice"), Join(s, "bob")
		a.Room, b.Room = door.A, door.A
		a.Turns = 5
		if _, err := Go(s, a, strconv.Itoa(door.B)); !IsOops(err) {
			t.Fatalf("passed a plate door with nobody on the plate: %v", err)
		}
		b.Room = door.Plate
		if _, err := Go(s, a, strconv.Itoa(door.B)); err != nil {
			t.Fatalf("plate held but door shut: %v", err)
		}
		return
	}
	t.Skip("no plate doors in sampled seeds")
}

// TestSeasonCanBeWon plays whole seasons with two players who never run out
// of turns, to prove every generated dungeon can be finished with the rules.
func TestSeasonCanBeWon(t *testing.T) {
	for seed := uint64(0); seed < 100; seed++ {
		s := newState(seed)
		a, b := Join(s, "alice"), Join(s, "bob")
		for depth := 1; depth <= world.Depths; depth++ {
			explore(t, s, a)
			box := s.FindKind(depth, world.Tarball)
			walk(t, s, a, box.Num)
			walk(t, s, b, box.Num)
			a.Turns = 99
			if _, err := Open(s, a); err != nil {
				t.Fatalf("seed %d depth %d: open: %v", seed, depth, err)
			}
			stairs := s.FindKind(depth, world.Stairs)
			walk(t, s, a, stairs.Num)
			walk(t, s, b, stairs.Num)
			for _, p := range []*world.Player{a, b} {
				if depth == world.Depths && p == b {
					continue
				}
				p.Turns = 99
				if _, err := Descend(s, p); err != nil {
					t.Fatalf("seed %d depth %d: descend %s: %v", seed, depth, p.User, err)
				}
			}
		}
		if !s.Won() {
			t.Fatalf("seed %d: season not won", seed)
		}
	}
}

// explore visits and searches every room it can reach, unlocking doors with
// found keys, until nothing changes.
func explore(t *testing.T, s *world.State, p *world.Player) {
	t.Helper()
	searched := map[int]bool{}
	for changed := true; changed; {
		changed = false
		for num := range reachable(s, p.Room) {
			if !searched[num] {
				walk(t, s, p, num)
				p.Turns = 99
				if _, err := Search(s, p); err != nil {
					t.Fatal(err)
				}
				searched[num] = true
				changed = true
			}
		}
		if s.Holding(p.User, world.Key) == nil {
			continue
		}
		for num := range reachable(s, p.Room) {
			for _, d := range s.DoorsOf(num) {
				if d.Revealed && d.Kind == world.Locked && s.Holding(p.User, world.Key) != nil {
					walk(t, s, p, num)
					p.Turns = 99
					if _, err := Use(s, p, world.Key, strconv.Itoa(d.Other(num))); err != nil {
						t.Fatal(err)
					}
					changed = true
				}
			}
		}
	}
}

func reachable(s *world.State, from int) map[int]int {
	prev := map[int]int{from: 0}
	queue := []int{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range s.DoorsOf(cur) {
			o := d.Other(cur)
			if _, seen := prev[o]; seen || !d.Revealed || d.Kind != world.Open {
				continue
			}
			prev[o] = cur
			queue = append(queue, o)
		}
	}
	return prev
}

func walk(t *testing.T, s *world.State, p *world.Player, to int) {
	t.Helper()
	prev := reachable(s, p.Room)
	if _, ok := prev[to]; !ok {
		t.Fatalf("room %d unreachable from %d", to, p.Room)
	}
	var path []int
	for cur := to; cur != p.Room; cur = prev[cur] {
		path = append([]int{cur}, path...)
	}
	for _, num := range path {
		p.Turns = 99
		if _, err := Go(s, p, strconv.Itoa(num)); err != nil {
			t.Fatalf("walk to %d: %v", num, err)
		}
	}
}
