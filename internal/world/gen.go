package world

import (
	"math/rand/v2"
	"slices"
)

// Generate builds a full season dungeon from a seed. The same seed always
// gives the same dungeon.
func Generate(seed uint64) ([]*Room, []*Door, []*Item) {
	g := &gen{}
	for depth := 1; depth <= Depths; depth++ {
		g.r = rand.New(rand.NewPCG(seed, uint64(depth)))
		g.depth(depth)
	}
	return g.rooms, g.doors, g.items
}

type cell struct{ x, y int }

type gen struct {
	r     *rand.Rand
	rooms []*Room
	doors []*Door
	items []*Item
}

func (g *gen) item(kind string, room int, hidden, inBox bool) {
	g.items = append(g.items, &Item{ID: len(g.items) + 1, Kind: kind, Room: room, Hidden: hidden, InBox: inBox})
}

func (g *gen) depth(depth int) {
	r := g.r
	target := 10 + r.IntN(5)

	// Grow a tree of rooms outward from an entrance on the west edge.
	start := cell{0, r.IntN(GridH)}
	at := map[cell]*Room{}
	parent := map[*Room]*Room{}
	var order []*Room
	add := func(c cell, from *Room) {
		rm := &Room{Num: len(g.rooms) + 1, Depth: depth, X: c.x, Y: c.y}
		g.rooms = append(g.rooms, rm)
		at[c] = rm
		parent[rm] = from
		order = append(order, rm)
	}
	add(start, nil)
	for len(order) < target {
		from := order[r.IntN(len(order))]
		var free []cell
		for _, d := range [][2]int{{0, -1}, {0, 1}, {1, 0}, {-1, 0}} {
			c := cell{from.X + d[0], from.Y + d[1]}
			if c.x >= 0 && c.x < GridW && c.y >= 0 && c.y < GridH && at[c] == nil {
				free = append(free, c)
			}
		}
		if len(free) > 0 {
			add(free[r.IntN(len(free))], from)
		}
	}

	entrance := order[0]
	entrance.Kind = Entrance
	children := map[*Room][]*Room{}
	for _, rm := range order[1:] {
		children[parent[rm]] = append(children[parent[rm]], rm)
	}

	// Tree distance from the entrance; the farthest room gets the stairs.
	dist := map[*Room]int{entrance: 0}
	for _, rm := range order[1:] {
		dist[rm] = dist[parent[rm]] + 1
	}
	stairs := order[1]
	for _, rm := range order[1:] {
		if dist[rm] > dist[stairs] {
			stairs = rm
		}
	}
	stairs.Kind = Stairs

	// The tarball sits in a dead end.
	var leaves, others []*Room
	for _, rm := range order {
		if rm.Kind != Plain {
			continue
		}
		others = append(others, rm)
		if len(children[rm]) == 0 {
			leaves = append(leaves, rm)
		}
	}
	box := pick(r, leaves)
	if box == nil {
		box = pick(r, others)
	}
	box.Kind = Tarball

	// Tree doors are open, except that one may be locked, gating a branch.
	treeDoor := map[*Room]*Door{}
	for _, rm := range order[1:] {
		d := g.door(parent[rm], rm, Open)
		treeDoor[rm] = d
	}
	gated := map[*Room]bool{}
	locks := 0
	if r.Float64() < 0.6 {
		var cands []*Room
		for _, rm := range order[2:] {
			if n := len(subtree(rm, children)); n >= 2 && n <= target/2 {
				cands = append(cands, rm)
			}
		}
		if rm := pick(r, cands); rm != nil {
			treeDoor[rm].Kind = Locked
			locks++
			for _, c := range subtree(rm, children) {
				gated[c] = true
			}
		}
	}
	var reachable []*Room // reachable without passing the gated branch
	for _, rm := range order {
		if !gated[rm] {
			reachable = append(reachable, rm)
		}
	}

	// Extra doors make loops: shortcuts that may be locked, plated or hidden.
	for _, a := range order {
		for _, d := range [][2]int{{1, 0}, {0, 1}} {
			b := at[cell{a.X + d[0], a.Y + d[1]}]
			if b == nil || parent[a] == b || parent[b] == a || r.Float64() > 0.3 {
				continue
			}
			dr := g.door(a, b, Open)
			switch roll := r.Float64(); {
			case roll < 0.25:
				dr.Kind = Locked
				locks++
			case roll < 0.5:
				var plates []*Room
				for _, p := range reachable {
					if p != a && p != b {
						plates = append(plates, p)
					}
				}
				if p := pick(r, plates); p != nil {
					dr.Kind = Plate
					dr.Plate = p.Num
				}
			}
			dr.Hidden = r.Float64() < 0.3
		}
	}

	// Murk sources grow in number with depth and avoid the important rooms.
	var murkable []*Room
	for _, rm := range order {
		if dist[rm] >= 2 && rm.Kind == Plain {
			murkable = append(murkable, rm)
		}
	}
	r.Shuffle(len(murkable), func(i, j int) { murkable[i], murkable[j] = murkable[j], murkable[i] })
	for _, rm := range murkable[:min(len(murkable), 1+(depth-1)/2)] {
		rm.Source = true
		rm.Murk = true
	}

	// Hidden packages. Keys never hide behind the lock they open.
	var stash, keyStash []*Room
	for _, rm := range order {
		if rm != entrance {
			stash = append(stash, rm)
			if !gated[rm] {
				keyStash = append(keyStash, rm)
			}
		}
	}
	for range locks + 1 {
		g.item(Key, pick(r, keyStash).Num, true, false)
	}
	g.item(Torch, pick(r, stash).Num, true, false)
	g.item(Torch, pick(r, stash).Num, true, false)
	g.item(Rope, pick(r, stash).Num, true, false)

	if depth == Depths {
		g.item(Relic, box.Num, false, true)
	} else {
		g.item(Torch, box.Num, false, true)
		g.item(Rope, box.Num, false, true)
		g.item(Key, box.Num, false, true)
	}
}

func (g *gen) door(a, b *Room, kind string) *Door {
	lo, hi := a.Num, b.Num
	if lo > hi {
		lo, hi = hi, lo
	}
	d := &Door{A: lo, B: hi, Kind: kind}
	g.doors = append(g.doors, d)
	return d
}

func subtree(rm *Room, children map[*Room][]*Room) []*Room {
	out := []*Room{rm}
	for _, c := range children[rm] {
		out = append(out, subtree(c, children)...)
	}
	return out
}

func pick[T any](r *rand.Rand, xs []T) T {
	var zero T
	if len(xs) == 0 {
		return zero
	}
	return xs[r.IntN(len(xs))]
}

// NewSeason returns a fresh state for a season, keeping the players.
func NewSeason(s *State, id int, seed uint64) {
	s.Season = Season{ID: id, Seed: seed, StartTick: s.Tick, StartedAt: s.Now}
	s.Rooms, s.Doors, s.Items = Generate(seed)
	s.FreshWorld = true
	camp := s.Camp()
	for _, p := range s.Players {
		p.Room, p.HP, p.Downed = camp.Num, MaxHP, false
	}
	Reveal(s, camp)
}

// Reveal marks a room as explored and shows its visible doors.
func Reveal(s *State, rm *Room) bool {
	was := rm.Revealed
	rm.Revealed = true
	for _, d := range s.DoorsOf(rm.Num) {
		if !d.Hidden {
			d.Revealed = true
		}
	}
	return !was
}

// DepthRooms returns rooms of a depth in grid order, for stable output.
func (s *State) DepthRooms(depth int) []*Room {
	var out []*Room
	for _, r := range s.Rooms {
		if r.Depth == depth {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b *Room) int { return (a.Y*GridW + a.X) - (b.Y*GridW + b.X) })
	return out
}
