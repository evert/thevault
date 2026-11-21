package game

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"thevault/internal/world"
)

var t0 = time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC)

func newState(seed uint64) *world.State {
	s := &world.State{Now: t0, Players: map[string]*world.Player{}, Rng: rand.New(rand.NewPCG(seed, 7))}
	Start(s, seed)
	return s
}

func seq() func() uint64 {
	n := uint64(1000)
	return func() uint64 { n++; return n }
}

// besides puts a player on the tile left of an enemy (moving it if needed).
func besides(s *world.State, p *world.Player, e *world.Enemy) {
	rm := s.Room(e.Room)
	for y := 1; y < world.RoomH-1; y++ {
		for x := 2; x < world.RoomW-1; x++ {
			if rm.At(x, y) == world.Floor && rm.At(x-1, y) == world.Floor && s.EnemyAt(rm.Num, x-1, y) == nil {
				e.X, e.Y = x, y
				p.Room, p.X, p.Y = rm.Num, x-1, y
				return
			}
		}
	}
	panic("no room to stand")
}

func TestPlayersStartInDifferentRooms(t *testing.T) {
	s := newState(1)
	rooms := map[int]bool{}
	for _, u := range []string{"alice", "bob", "carol", "dave", "eve"} {
		p := Join(s, u)
		if rooms[p.Room] {
			t.Fatalf("%s shares room %d", u, p.Room)
		}
		rooms[p.Room] = true
		if s.Room(p.Room).Floor != 1 {
			t.Fatalf("%s started on floor %d", u, s.Room(p.Room).Floor)
		}
	}
	colors := map[int]bool{}
	for _, p := range s.Players {
		colors[p.Color] = true
	}
	if len(colors) != 5 {
		t.Fatalf("players share colours: %v", colors)
	}
}

func TestMoveCostsEnergyAndWallsAreFree(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	rm := s.Room(p.Room)
	p.X, p.Y = world.RoomW/2, world.RoomH/2
	s.Enemies = nil
	s.Items = nil
	if _, err := Move(s, p, "right"); err != nil || p.Energy != world.EnergyPerTick-1 || p.X != world.RoomW/2+1 {
		t.Fatalf("step: err=%v energy=%d x=%d", err, p.Energy, p.X)
	}
	p.X, p.Y = 1, 1
	if rm.At(1, 0) != world.Wall {
		t.Skip("door at corner")
	}
	before := p.Energy
	Move(s, p, "up")
	if p.Energy != before || p.Y != 1 {
		t.Fatal("bumping a wall cost energy or moved you")
	}
	p.Energy = 0
	p.X, p.Y = world.RoomW/2, world.RoomH/2
	if _, err := Move(s, p, "left"); !IsOops(err) {
		t.Fatalf("moved with no energy: %v", err)
	}
}

func TestDevEnergyRefillsWhenEmpty(t *testing.T) {
	DevEnergy = true
	defer func() { DevEnergy = false }()
	s := newState(1)
	p := Join(s, "alice")
	p.X, p.Y = world.RoomW/2, world.RoomH/2
	s.Enemies = nil
	s.Items = nil
	p.Energy = 0
	if _, err := Move(s, p, "right"); err != nil || p.Energy != world.MaxEnergy-1 {
		t.Fatalf("dev move: err=%v energy=%d", err, p.Energy)
	}
}

func TestDoorsLeadToTheNextRoom(t *testing.T) {
	s := newState(3)
	p := Join(s, "alice")
	s.Enemies = nil
	for _, d := range s.Doors {
		a, b := s.Room(d.A), s.Room(d.B)
		dir := ""
		for name, off := range world.Dirs {
			if a.X+off[0] == b.X && a.Y+off[1] == b.Y {
				dir = name
			}
		}
		dx, dy := world.DoorTile(dir)
		off := world.Dirs[dir]
		p.Room, p.X, p.Y = a.Num, dx-off[0], dy-off[1]
		if _, err := Move(s, p, dir); err != nil {
			t.Fatal(err)
		}
		if p.Room != b.Num {
			t.Fatalf("door %d→%d went to %d", a.Num, b.Num, p.Room)
		}
		if !b.Revealed {
			t.Fatal("entering a room didn't reveal it")
		}
		return
	}
}

func TestStairsGoDownAndUp(t *testing.T) {
	s := newState(4)
	p := Join(s, "alice")
	st := s.FindKind(1, world.Stairs)
	x, y := st.Find(world.StairDown)
	p.Room, p.X, p.Y = st.Num, x-1, y
	if st.At(x-1, y) != world.Floor {
		p.X, p.Y = x+1, y
	}
	dir := "right"
	if p.X > x {
		dir = "left"
	}
	s.Enemies = nil
	if _, err := Move(s, p, dir); err != nil {
		t.Fatal(err)
	}
	if s.Room(p.Room).Floor != 2 {
		t.Fatalf("on floor %d after stairs", s.Room(p.Room).Floor)
	}
}

func TestBattleKillSharesXP(t *testing.T) {
	s := newState(5)
	a, b := Join(s, "alice"), Join(s, "bob")
	e := s.Enemies[0]
	e.Boss = false
	besides(s, a, e)
	if _, err := Move(s, a, "right"); err != nil || a.Battle != e.ID {
		t.Fatalf("bump didn't start a fight: %v", err)
	}
	// bob joins from the same tile.
	b.Room, b.X, b.Y = a.Room, a.X, a.Y
	if _, err := Move(s, b, "right"); err != nil || b.Battle != e.ID {
		t.Fatal("bob couldn't join")
	}
	if n := len(s.Fighters(e.ID)); n != 2 {
		t.Fatalf("fighters = %d", n)
	}
	e.HP = 3
	b.HP = 100 // survive any counter
	if _, err := Attack(s, b); err != nil {
		t.Fatal(err)
	}
	a.HP = 100
	if s.Enemy(e.ID) != nil {
		Attack(s, a)
	}
	if s.Enemy(e.ID) != nil {
		t.Fatal("enemy survived")
	}
	if a.Battle != 0 || b.Battle != 0 {
		t.Fatal("fighters still in battle after the kill")
	}
	// Whoever hit it got XP; bob always did.
	if b.XP == 0 && b.Level == 1 {
		t.Fatal("bob got no XP")
	}
}

func TestTeamworkBonus(t *testing.T) {
	solo, team := 0, 0
	for i := range 200 {
		for _, n := range []int{1, 3} {
			s := newState(uint64(i))
			e := s.Enemies[0]
			e.HP, e.MaxHP, e.Boss = 10000, 10000, false
			var ps []*world.Player
			for j := range n {
				p := Join(s, string(rune('a'+j)))
				p.Battle, p.HP = e.ID, 1000
				ps = append(ps, p)
			}
			Attack(s, ps[0])
			if n == 1 {
				solo += 10000 - e.HP
			} else {
				team += 10000 - e.HP
			}
		}
	}
	if float64(team) < float64(solo)*1.3 {
		t.Fatalf("3-player damage %d not clearly above solo %d", team, solo)
	}
}

func TestKnockoutLastsUntilTick(t *testing.T) {
	s := newState(6)
	p := Join(s, "alice")
	e := s.Enemies[0]
	e.Atk = 1000
	p.Battle = e.ID
	for i := 0; i < 20 && !p.KO; i++ {
		Defend(s, p)
	}
	if !p.KO || p.Battle != 0 {
		t.Fatalf("expected KO: %+v", p)
	}
	if _, err := Move(s, p, "left"); !IsOops(err) {
		t.Fatal("KO'd player moved")
	}
	Tick(s, seq())
	if p.KO || p.HP != p.MaxHP() {
		t.Fatalf("not revived at tick: %+v", p)
	}
}

func TestTickRefillsEnergyAndHeals(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	p.Energy, p.HP = 150, 2
	Tick(s, seq())
	if p.Energy != world.MaxEnergy {
		t.Fatalf("energy = %d, want cap %d", p.Energy, world.MaxEnergy)
	}
	if p.HP != 2+p.MaxHP()/2 {
		t.Fatalf("hp = %d", p.HP)
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
		t.Fatalf("second catch-up ran %d ticks", n)
	}
}

func TestLevelUp(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	gainXP(s, p, 35) // 10 for Lv2, 20 for Lv3, 5 left
	if p.Level != 3 || p.XP != 5 || p.HP != world.MaxHPFor(3) {
		t.Fatalf("level %d xp %d hp %d", p.Level, p.XP, p.HP)
	}
}

func TestBossWinStartsNewSeason(t *testing.T) {
	s := newState(8)
	p := Join(s, "alice")
	var boss *world.Enemy
	for _, e := range s.Enemies {
		if e.Boss {
			boss = e
		}
	}
	p.Room, p.Battle, p.HP = boss.Room, boss.ID, 1000
	boss.HP = 1
	if _, err := Attack(s, p); err != nil {
		t.Fatal(err)
	}
	if !s.Won() || s.Season.Winner != "alice" {
		t.Fatal("season not won")
	}
	if _, err := Move(s, p, "left"); !IsOops(err) {
		t.Fatal("could act after the win")
	}
	Tick(s, seq())
	if s.Season.ID != 2 || s.Won() || p.Level != 1 || s.Room(p.Room).Floor != 1 {
		t.Fatalf("new season not set up: season %d level %d", s.Season.ID, p.Level)
	}
}

func TestPackages(t *testing.T) {
	s := newState(1)
	p := Join(s, "alice")
	s.Items = []*world.Item{{ID: 1, Kind: world.Patch, Holder: "alice"}, {ID: 2, Kind: world.Rimraf, Holder: "alice"}}
	p.HP = 1
	if _, err := UseOutside(s, p, world.Patch); err != nil || p.HP != 1+p.MaxHP()/2 {
		t.Fatalf("patch: %v hp=%d", err, p.HP)
	}
	if _, err := UseOutside(s, p, world.Rimraf); !IsOops(err) {
		t.Fatal("rimraf worked outside a fight")
	}
	e := s.Enemies[0]
	e.Boss = false
	p.Battle, p.HP = e.ID, 1000
	before := e.HP
	if _, err := UseInBattle(s, p, world.Rimraf); err != nil {
		t.Fatal(err)
	}
	if s.Enemy(e.ID) != nil && e.HP >= before {
		t.Fatal("rimraf did no damage")
	}
}

func TestFightingTeachesTheType(t *testing.T) {
	s := newState(9)
	a, b := Join(s, "alice"), Join(s, "bob")
	e := s.Enemies[0]
	e.Boss = false
	besides(s, a, e)
	if a.Known[e.Name] {
		t.Fatal("alice knew the type before fighting")
	}
	Move(s, a, "right")
	if !a.Known[e.Name] {
		t.Fatal("starting a fight didn't teach alice the type")
	}
	if b.Known[e.Name] {
		t.Fatal("bob learned a type he never fought")
	}
	Flee(s, a)
	if !a.Known[e.Name] {
		t.Fatal("fleeing made alice forget the type")
	}
}

// outsideLock puts a player next to a door into the floor's locked stairs
// room and returns the direction to walk.
func outsideLock(t *testing.T, s *world.State, p *world.Player, floor int) (string, *world.Room) {
	t.Helper()
	st := s.FindKind(floor, world.Stairs)
	for name, off := range world.Dirs {
		n := s.RoomAt(floor, st.X-off[0], st.Y-off[1])
		if n == nil || !s.Linked(n.Num, st.Num) {
			continue
		}
		dx, dy := world.DoorTile(name)
		p.Room, p.X, p.Y = n.Num, dx-off[0], dy-off[1]
		return name, st
	}
	t.Fatal("stairs room has no doors")
	return "", nil
}

func TestLockedDoorNeedsToken(t *testing.T) {
	s := newState(6)
	p := Join(s, "alice")
	s.Enemies = nil
	dir, st := outsideLock(t, s, p, 1)
	room, energy := p.Room, p.Energy
	if _, err := Move(s, p, dir); !IsOops(err) || !strings.Contains(err.Error(), "vlt_1") {
		t.Fatalf("walked into a locked door: %v", err)
	}
	if p.Room != room || p.Energy != energy {
		t.Fatal("bumping a locked door moved you or cost energy")
	}
	s.Items = append(s.Items, &world.Item{ID: s.NextItemID(), Kind: "vlt_1", Holder: "alice"})
	if _, err := Move(s, p, dir); err != nil {
		t.Fatal(err)
	}
	if p.Room != st.Num || st.Locked() || s.Holding("alice", "vlt_1") != nil {
		t.Fatalf("room=%d locked=%v key kept=%v", p.Room, st.Locked(), s.Holding("alice", "vlt_1") != nil)
	}
	// It stays open for everyone else.
	b := Join(s, "bob")
	dir, _ = outsideLock(t, s, b, 1)
	if _, err := Move(s, b, dir); err != nil || b.Room != st.Num {
		t.Fatalf("bob couldn't follow: %v", err)
	}
}

func TestTokenHints(t *testing.T) {
	s := newState(6)
	p := Join(s, "alice")
	s.Items, s.Enemies = nil, nil
	if h := lockedHint(s, p, 1); h != "Locked. It needs the vlt_1 token." {
		t.Fatalf("no token anywhere: %q", h)
	}
	rm := s.FloorRooms(1)[1]
	s.Items = append(s.Items, &world.Item{ID: 1, Kind: "vlt_1", Room: rm.Num, X: 1, Y: 1})
	if h := lockedHint(s, p, 1); !strings.Contains(h, "lying somewhere on this floor") {
		t.Fatalf("key on the floor: %q", h)
	}
	Join(s, "bob")
	s.Items[0].Holder = "bob"
	if h := lockedHint(s, p, 1); !strings.Contains(h, "bob has it") {
		t.Fatalf("bob holds it: %q", h)
	}
}

func TestMonsterDropsToken(t *testing.T) {
	s := newState(7)
	p := Join(s, "alice")
	var e *world.Enemy
	for _, o := range s.Enemies {
		if !o.Boss {
			e = o
			break
		}
	}
	e.Key, e.HP = true, 1
	besides(s, p, e)
	if _, err := Move(s, p, "right"); err != nil {
		t.Fatal(err)
	}
	msg, err := Attack(s, p)
	if err != nil {
		t.Fatal(err)
	}
	want := world.Token(s.Room(e.Room).Floor)
	if s.Holding("alice", want) == nil || !strings.Contains(msg, want) {
		t.Fatalf("no %s from the carrier: %q", want, msg)
	}
}

func TestKnockoutDropsToken(t *testing.T) {
	s := newState(8)
	p := Join(s, "alice")
	e := s.Enemies[0]
	e.Atk = 1000
	besides(s, p, e)
	Move(s, p, "right")
	s.Items = append(s.Items, &world.Item{ID: s.NextItemID(), Kind: "vlt_1", Holder: "alice"})
	for !p.KO {
		p.HP = 1
		Defend(s, p)
	}
	key := s.Items[len(s.Items)-1]
	if key.Holder != "" || key.Room != p.Room {
		t.Fatalf("key after KO: holder=%q room=%d", key.Holder, key.Room)
	}
	if s.ItemAt(key.Room, key.X, key.Y) != key {
		t.Fatal("dropped key isn't on the map")
	}
	if key.X == p.X && key.Y == p.Y {
		t.Fatal("key dropped under the player, where their icon hides it")
	}
	if dx, dy := key.X-p.X, key.Y-p.Y; max(dx, -dx, dy, -dy) != 1 {
		t.Fatalf("key dropped at %d,%d, not next to the player at %d,%d", key.X, key.Y, p.X, p.Y)
	}
}

func TestIdlePlayerDropsToken(t *testing.T) {
	s := newState(9)
	a, b := Join(s, "alice"), Join(s, "bob")
	s.Items = append(s.Items,
		&world.Item{ID: s.NextItemID(), Kind: "vlt_1", Holder: "alice"},
		&world.Item{ID: s.NextItemID() + 1, Kind: "vlt_2", Holder: "bob"})
	a.LastSeen = s.Now.Add(-KeyIdle)
	b.LastSeen = s.Now.Add(-KeyIdle + time.Minute)
	DropIdleKeys(s)
	if s.Holding("alice", "vlt_1") != nil {
		t.Fatal("alice kept her key while away")
	}
	if s.Holding("bob", "vlt_2") == nil {
		t.Fatal("bob lost his key a minute early")
	}
}

func TestNobodyStartsInALockedRoom(t *testing.T) {
	for seed := uint64(1); seed < 30; seed++ {
		s := newState(seed)
		for i := range 12 {
			p := Join(s, fmt.Sprintf("user%d", i))
			if s.Room(p.Room).Locked() {
				t.Fatalf("seed %d: %s started in the locked room", seed, p.User)
			}
		}
	}
}
