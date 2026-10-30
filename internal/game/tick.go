// Package game holds the rules: the daily tick, moving and fighting.
package game

import (
	"errors"
	"fmt"
	"time"

	"thevault/internal/clock"
	"thevault/internal/world"
)

// MaxCatchUp limits how many missed days are replayed at once. Energy caps
// at world.MaxEnergy anyway.
const MaxCatchUp = 7

// KeyIdle is how long someone can be away before the tick takes their tokens
// off them and leaves them where they stood.
const KeyIdle = 48 * time.Hour

// Oops is a player mistake: shown to them, but the transaction still commits.
type Oops string

func (o Oops) Error() string { return string(o) }

func oops(format string, a ...any) error { return Oops(fmt.Sprintf(format, a...)) }

func IsOops(err error) bool {
	var o Oops
	return errors.As(err, &o)
}

// Player colours: distinct, readable on dark and light terminals.
var palette = []int{196, 208, 226, 46, 51, 33, 129, 201, 214, 118, 45, 171}

// Start creates the very first world.
func Start(s *world.State, seed uint64) {
	s.Tick = 1
	s.LastTick = clock.LatestBoundary(s.Now)
	NewSeason(s, 1, seed)
}

// NewSeason generates a fresh dungeon and sends everyone back to floor 1,
// each to their own room where possible. Levels reset each season.
func NewSeason(s *world.State, id int, seed uint64) {
	s.Season = world.Season{ID: id, Seed: seed, StartTick: s.Tick, StartedAt: s.Now}
	s.Rooms, s.Doors, s.Enemies, s.Items = world.Generate(seed)
	s.EnemySeq = len(s.Enemies)
	s.FreshWorld = true
	for _, u := range s.Users() {
		p := s.Players[u]
		p.Level, p.XP, p.Battle, p.KO, p.Braced = 1, 0, 0, false, false
		p.HP = p.MaxHP()
		p.Energy = max(p.Energy, world.EnergyPerTick)
		spawn(s, p)
	}
	s.Emit("", "season", fmt.Sprintf("Season %d begins. The Vault is open.", id))
}

// spawn puts a player in the emptiest room on floor 1.
func spawn(s *world.State, p *world.Player) {
	var best []*world.Room
	fewest := 1 << 30
	for _, rm := range s.FloorRooms(1) {
		if rm.Locked() {
			continue // no starting behind the locked door
		}
		// Other players matter most; enemies only break ties.
		n := 0
		for _, o := range s.Players {
			if o != p && o.Room == rm.Num {
				n += 100
			}
		}
		for _, e := range s.Enemies {
			if e.Room == rm.Num {
				n++
			}
		}
		switch {
		case n < fewest:
			best, fewest = []*world.Room{rm}, n
		case n == fewest:
			best = append(best, rm)
		}
	}
	rm := best[s.Rng.IntN(len(best))]
	p.Room = rm.Num
	p.X, p.Y = s.FreeTile(rm)
	rm.Revealed = true
}

// Join adds a player the first time they play.
func Join(s *world.State, user string) *world.Player {
	if p, ok := s.Players[user]; ok {
		return p
	}
	used := map[int]bool{}
	for _, o := range s.Players {
		used[o.Color] = true
	}
	var free []int
	for _, c := range palette {
		if !used[c] {
			free = append(free, c)
		}
	}
	if len(free) == 0 {
		free = palette
	}
	p := &world.Player{User: user, Color: free[s.Rng.IntN(len(free))], Level: 1,
		Energy: world.EnergyPerTick, Joined: s.Now, LastSeen: s.Now, Known: map[string]bool{}}
	p.HP = p.MaxHP()
	s.Players[user] = p
	spawn(s, p)
	s.Emit(user, "join", fmt.Sprintf("%s entered the Vault.", user))
	return p
}

// CatchUp runs a tick for every 3 AM boundary passed since the last one.
func CatchUp(s *world.State, newSeed func() uint64) int {
	missed := clock.Missed(s.LastTick, s.Now, MaxCatchUp)
	now := s.Now
	for _, b := range missed {
		s.Now = b
		Tick(s, newSeed)
	}
	s.Now = now
	if len(missed) > 0 {
		s.LastTick = clock.LatestBoundary(now)
	}
	return len(missed)
}

// Tick advances the world by one day.
func Tick(s *world.State, newSeed func() uint64) {
	s.Tick++
	if s.Won() {
		NewSeason(s, s.Season.ID+1, newSeed())
		return
	}

	for _, u := range s.Users() {
		p := s.Players[u]
		// Someone who's stopped playing shouldn't sit on a token for days.
		if s.Now.Sub(p.LastSeen) > KeyIdle && dropKeys(s, p) > 0 {
			s.Emit(u, "key", fmt.Sprintf("%s hasn't been around, so their token was left in room %d.", u, p.Room))
		}
		if p.KO {
			ent := floorStart(s, s.Room(p.Room).Floor)
			p.KO, p.Battle, p.Braced = false, 0, false
			p.HP = p.MaxHP()
			p.Room = ent.Num
			p.X, p.Y = s.FreeTile(ent)
			s.Emit(u, "wake", fmt.Sprintf("%s came to in room %d, patched up and grumpy.", u, ent.Num))
		} else {
			p.HP = min(p.MaxHP(), p.HP+p.MaxHP()/2)
		}
		p.Energy = min(world.MaxEnergy, p.Energy+world.EnergyPerTick)
	}

	for _, e := range s.Enemies {
		regen := e.MaxHP / 4
		if e.Boss {
			regen = e.MaxHP / 10
		}
		e.HP = min(e.MaxHP, e.HP+regen)
	}

	respawn(s)
}

// respawn adds a couple of fresh enemies on each floor someone has reached,
// in explored rooms nobody is standing in.
func respawn(s *world.State) {
	for floor := 1; floor <= world.Floors; floor++ {
		var rooms []*world.Room
		count := 0
		for _, rm := range s.FloorRooms(floor) {
			if rm.Revealed && len(s.PlayersIn(rm.Num)) == 0 && rm.Kind != world.Entrance {
				rooms = append(rooms, rm)
			}
		}
		for _, e := range s.Enemies {
			if s.Room(e.Room).Floor == floor {
				count++
			}
		}
		if len(rooms) == 0 || count >= 2*len(s.FloorRooms(floor)) {
			continue
		}
		for range 2 {
			rm := rooms[s.Rng.IntN(len(rooms))]
			e := world.NewEnemy(s.Rng, s.NextEnemyID(), floor)
			e.Room = rm.Num
			e.X, e.Y = s.FreeTile(rm)
			s.Enemies = append(s.Enemies, e)
		}
	}
}

// floorStart is where you wake up on a floor: its entrance, or on floor 1
// the first room.
func floorStart(s *world.State, floor int) *world.Room {
	if rm := s.FindKind(floor, world.Entrance); rm != nil {
		return rm
	}
	return s.FloorRooms(floor)[0]
}

// NewSeed returns a seed for a new season.
func NewSeed() uint64 { return uint64(time.Now().UnixNano()) }
