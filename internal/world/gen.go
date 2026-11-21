package world

import (
	"math/rand/v2"
	"slices"
)

// BossName is the final enemy, on the last floor.
var BossName = Boss.Name

const BossLevel = 9

// EnemyStats gives an enemy's max HP and attack. Below floor 1 enemies get
// 10% more HP per floor and a little more bite.
func EnemyStats(floor, level int, boss bool) (hp, atk int) {
	hp, atk = 8+6*level, 1+3*level/2
	if boss {
		hp, atk = hp*4, atk*13/10
	}
	hp = hp * (10 + floor - 1) / 10
	atk += floor / 2
	return
}

// RoomEnemies is how many enemies a room starts (or refills) with.
func RoomEnemies(r *rand.Rand) int { return 1 + r.IntN(2) }

// NewEnemy makes an enemy for a floor.
func NewEnemy(r *rand.Rand, id, floor int) *Enemy {
	level := floor + r.IntN(2)
	tier := Tiers[min(floor, Floors)-1]
	hp, atk := EnemyStats(floor, level, false)
	return &Enemy{ID: id, Name: tier[r.IntN(len(tier))].Name, Level: level, HP: hp, MaxHP: hp, Atk: atk, Hitters: map[string]bool{}}
}

// Generate builds a whole season's dungeon from a seed. The same seed always
// gives the same dungeon.
func Generate(seed uint64) ([]*Room, []DoorLink, []*Enemy, []*Item) {
	var rooms []*Room
	var doors []DoorLink
	for floor := 1; floor <= Floors; floor++ {
		r := rand.New(rand.NewPCG(seed, uint64(floor)))
		fr, fd := genFloor(r, floor, len(rooms))
		rooms = append(rooms, fr...)
		doors = append(doors, fd...)
	}
	BuildLayouts(seed, rooms, doors)

	var enemies []*Enemy
	var items []*Item
	for floor := 1; floor <= Floors; floor++ {
		r := rand.New(rand.NewPCG(seed, uint64(100+floor)))
		var floorRooms []*Room
		for _, rm := range rooms {
			if rm.Floor == floor {
				floorRooms = append(floorRooms, rm)
			}
		}
		for i, rm := range floorRooms {
			taken := map[[2]int]bool{}
			place := func() (int, int) {
				for {
					x, y := 1+r.IntN(RoomW-2), 1+r.IntN(RoomH-2)
					if rm.Tiles[y][x] == Floor && !taken[[2]int{x, y}] && !nearDoor(rm, x, y) {
						taken[[2]int{x, y}] = true
						return x, y
					}
				}
			}
			switch {
			case floor == Floors && rm.Kind == Stairs:
				hp, atk := EnemyStats(floor, BossLevel, true)
				x, y := place()
				enemies = append(enemies, &Enemy{ID: len(enemies) + 1, Room: rm.Num, X: x, Y: y, Name: BossName,
					Level: BossLevel, HP: hp, MaxHP: hp, Atk: atk, Boss: true, Hitters: map[string]bool{}})
			case i > 0: // the first room on each floor starts calm
				for range RoomEnemies(r) {
					e := NewEnemy(r, len(enemies)+1, floor)
					e.Room = rm.Num
					e.X, e.Y = place()
					enemies = append(enemies, e)
				}
			}
			if r.IntN(2) == 0 {
				x, y := place()
				items = append(items, &Item{ID: len(items) + 1, Kind: RandomPackage(r), Room: rm.Num, X: x, Y: y})
			}
		}
	}
	// Tokens get their own random stream so adding them didn't change any
	// existing dungeon.
	for floor := 1; floor < Floors; floor++ {
		items = placeKey(rand.New(rand.NewPCG(seed, uint64(200+floor))), floor, rooms, enemies, items)
	}
	return rooms, doors, enemies, items
}

// placeKey hides a floor's token: half the time a monster carries it,
// otherwise it lies in a room. Never in the floor's first room or in the locked one.
func placeKey(r *rand.Rand, floor int, rooms []*Room, enemies []*Enemy, items []*Item) []*Item {
	var spots []*Room
	ok := map[int]bool{}
	first := true
	for _, rm := range rooms {
		if rm.Floor != floor {
			continue
		}
		if !first && rm.Kind != Stairs {
			spots = append(spots, rm)
			ok[rm.Num] = true
		}
		first = false
	}
	var carriers []*Enemy
	for _, e := range enemies {
		if ok[e.Room] && !e.Boss {
			carriers = append(carriers, e)
		}
	}
	if len(carriers) > 0 && r.IntN(2) == 0 {
		carriers[r.IntN(len(carriers))].Key = true
		return items
	}
	rm := spots[r.IntN(len(spots))]
	var free [][2]int
	for y := 1; y < RoomH-1; y++ {
		for x := 1; x < RoomW-1; x++ {
			if rm.Tiles[y][x] == Floor && !nearDoor(rm, x, y) && !taken(rm.Num, x, y, enemies, items) {
				free = append(free, [2]int{x, y})
			}
		}
	}
	c := [2]int{RoomW / 2, RoomH / 2}
	if len(free) > 0 {
		c = free[r.IntN(len(free))]
	}
	return append(items, &Item{ID: len(items) + 1, Kind: Token(floor), Room: rm.Num, X: c[0], Y: c[1]})
}

func taken(room, x, y int, enemies []*Enemy, items []*Item) bool {
	for _, e := range enemies {
		if e.Room == room && e.X == x && e.Y == y {
			return true
		}
	}
	for _, it := range items {
		if it.Holder == "" && it.Room == room && it.X == x && it.Y == y {
			return true
		}
	}
	return false
}

// RandomPackage picks a package kind: patches are the most common.
func RandomPackage(r *rand.Rand) string {
	switch n := r.IntN(4); {
	case n < 2:
		return Patch
	case n == 2:
		return Rimraf
	}
	return Cache
}

// genFloor grows a tree of rooms across the floor grid, then adds a few loops.
func genFloor(r *rand.Rand, floor, numBase int) ([]*Room, []DoorLink) {
	target := 8 + r.IntN(4)
	at := map[[2]int]*Room{}
	parent := map[*Room]*Room{}
	var order []*Room
	var doors []DoorLink
	link := func(a, b *Room) {
		lo, hi := min(a.Num, b.Num), max(a.Num, b.Num)
		doors = append(doors, DoorLink{lo, hi})
	}
	add := func(x, y int, from *Room) {
		rm := &Room{Num: numBase + len(order) + 1, Floor: floor, X: x, Y: y}
		at[[2]int{x, y}] = rm
		parent[rm] = from
		order = append(order, rm)
		if from != nil {
			link(from, rm)
		}
	}
	add(0, r.IntN(FloorH), nil)
	for len(order) < target {
		from := order[r.IntN(len(order))]
		var free [][2]int
		for _, d := range Dirs {
			c := [2]int{from.X + d[0], from.Y + d[1]}
			if c[0] >= 0 && c[0] < FloorW && c[1] >= 0 && c[1] < FloorH && at[c] == nil {
				free = append(free, c)
			}
		}
		if len(free) > 0 {
			c := pickCell(r, free)
			add(c[0], c[1], from)
		}
	}
	for _, a := range order {
		for _, d := range [][2]int{{1, 0}, {0, 1}} {
			b := at[[2]int{a.X + d[0], a.Y + d[1]}]
			if b != nil && parent[a] != b && parent[b] != a && r.Float64() < 0.3 {
				link(a, b)
			}
		}
	}
	if floor > 1 {
		order[0].Kind = Entrance
	}
	// The stairs go in the room farthest from the start.
	dist := map[*Room]int{order[0]: 0}
	far := order[1]
	for _, rm := range order[1:] {
		dist[rm] = dist[parent[rm]] + 1
		if dist[rm] > dist[far] {
			far = rm
		}
	}
	far.Kind = Stairs
	return order, doors
}

// pickCell picks a random cell, sorting first so the result is reproducible
// (the candidates come from iterating a map).
func pickCell(r *rand.Rand, cells [][2]int) [2]int {
	slices.SortFunc(cells, func(a, b [2]int) int { return (a[1]*FloorW + a[0]) - (b[1]*FloorW + b[0]) })
	return cells[r.IntN(len(cells))]
}

// Door tile positions in the walls: the middle of each side.
func DoorTile(dir string) (int, int) {
	switch dir {
	case "up":
		return RoomW / 2, 0
	case "down":
		return RoomW / 2, RoomH - 1
	case "left":
		return 0, RoomH / 2
	}
	return RoomW - 1, RoomH / 2
}

// Opposite returns the reverse direction.
func Opposite(dir string) string {
	return map[string]string{"up": "down", "down": "up", "left": "right", "right": "left"}[dir]
}

// DoorDir returns which wall of a room the tile (x, y) is a door in.
func DoorDir(x, y int) string {
	for _, d := range []string{"up", "down", "left", "right"} {
		if dx, dy := DoorTile(d); dx == x && dy == y {
			return d
		}
	}
	return ""
}

func nearDoor(rm *Room, x, y int) bool {
	for _, d := range Dirs {
		if rm.At(x+d[0], y+d[1]) == Door {
			return true
		}
	}
	return false
}

// BuildLayouts fills in every room's tiles. It's deterministic, so layouts
// are rebuilt on load instead of being stored.
func BuildLayouts(seed uint64, rooms []*Room, doors []DoorLink) {
	byNum := map[int]*Room{}
	for _, rm := range rooms {
		byNum[rm.Num] = rm
	}
	for _, rm := range rooms {
		exits := map[string]bool{}
		for _, d := range doors {
			if d.A != rm.Num && d.B != rm.Num {
				continue
			}
			o := byNum[d.Other(rm.Num)]
			for name, off := range Dirs {
				if rm.X+off[0] == o.X && rm.Y+off[1] == o.Y {
					exits[name] = true
				}
			}
		}
		layout(rand.New(rand.NewPCG(seed, uint64(10000+rm.Num))), rm, exits)
	}
}

func layout(r *rand.Rand, rm *Room, exits map[string]bool) {
	t := &rm.Tiles
	for y := range RoomH {
		for x := range RoomW {
			if x == 0 || y == 0 || x == RoomW-1 || y == RoomH-1 {
				t[y][x] = Wall
			} else {
				t[y][x] = Floor
			}
		}
	}
	for _, d := range []string{"up", "down", "left", "right"} {
		if exits[d] {
			x, y := DoorTile(d)
			t[y][x] = Door
		}
	}
	// Pillars and rubble, keeping the middle row and column clear so every
	// door connects.
	for range r.IntN(6) {
		px, py := 2+r.IntN(RoomW-4), 2+r.IntN(RoomH-4)
		w, h := 1+r.IntN(3), 1+r.IntN(2)
		for y := py; y < min(py+h, RoomH-2); y++ {
			for x := px; x < min(px+w, RoomW-2); x++ {
				if x != RoomW/2 && y != RoomH/2 {
					t[y][x] = Wall
				}
			}
		}
	}
	// Wall off any pocket you couldn't walk to.
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
	var spots [][2]int
	for y := 1; y < RoomH-1; y++ {
		for x := 1; x < RoomW-1; x++ {
			if t[y][x] == Floor && !seen[[2]int{x, y}] {
				t[y][x] = Wall
			}
			if t[y][x] == Floor && x != RoomW/2 && y != RoomH/2 && !nearDoor(rm, x, y) {
				spots = append(spots, [2]int{x, y})
			}
		}
	}
	stair := func(tile byte) {
		if len(spots) == 0 {
			t[1][1] = tile // can't happen with the clear cross, but stay safe
			return
		}
		c := spots[r.IntN(len(spots))]
		t[c[1]][c[0]] = tile
	}
	if rm.Kind == Stairs && rm.Floor < Floors {
		stair(StairDown)
	}
	if rm.Kind == Entrance {
		stair(StairUp)
	}
}

// FreeTile returns a random walkable tile with nothing on it.
func (s *State) FreeTile(rm *Room) (int, int) {
	var spots [][2]int
	for y := 1; y < RoomH-1; y++ {
		for x := 1; x < RoomW-1; x++ {
			if rm.Tiles[y][x] == Floor && s.EnemyAt(rm.Num, x, y) == nil && !nearDoor(rm, x, y) && len(s.playersAt(rm.Num, x, y)) == 0 {
				spots = append(spots, [2]int{x, y})
			}
		}
	}
	if len(spots) == 0 {
		return RoomW / 2, RoomH / 2
	}
	c := spots[s.Rng.IntN(len(spots))]
	return c[0], c[1]
}

// NearTile returns a free walkable tile next to (x, y), for arriving by stairs.
func (s *State) NearTile(rm *Room, x, y int) (int, int) {
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
		nx, ny := x+d[0], y+d[1]
		if rm.At(nx, ny) == Floor && s.EnemyAt(rm.Num, nx, ny) == nil {
			return nx, ny
		}
	}
	return s.FreeTile(rm)
}

// DropTile finds the nearest walkable tile next to (x, y) with nobody and
// nothing on it, for dropping things. It never picks (x, y) itself, so a
// player's icon can't hide what they dropped.
func (s *State) DropTile(rm *Room, x, y int) (int, int) {
	for r := 1; r < RoomW; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				nx, ny := x+dx, y+dy
				if max(abs(dx), abs(dy)) == r && rm.At(nx, ny) == Floor && len(s.playersAt(rm.Num, nx, ny)) == 0 &&
					s.ItemAt(rm.Num, nx, ny) == nil && s.EnemyAt(rm.Num, nx, ny) == nil {
					return nx, ny
				}
			}
		}
	}
	return s.FreeTile(rm)
}

func abs(n int) int { return max(n, -n) }

func (s *State) playersAt(room, x, y int) []*Player {
	var out []*Player
	for _, p := range s.Players {
		if p.Room == room && p.X == x && p.Y == y {
			out = append(out, p)
		}
	}
	return out
}
