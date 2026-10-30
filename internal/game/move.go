package game

import (
	"fmt"

	"thevault/internal/clock"
	"thevault/internal/world"
)

// canAct checks the things that stop any action.
func canAct(s *world.State, p *world.Player) error {
	switch {
	case s.Won():
		return oops("This season is won! A new one starts at 3 AM ET (in %s).", clock.Until(s.Now))
	case p.KO:
		return oops("You're knocked out. You'll come to at 3 AM ET (in %s).", clock.Until(s.Now))
	}
	return nil
}

func spend(s *world.State, p *world.Player, cost int) error {
	if p.Energy < cost {
		return oops("Out of energy. You get %d more at 3 AM ET (in %s).", world.EnergyPerTick, clock.Until(s.Now))
	}
	p.Energy -= cost
	return nil
}

// Move steps one tile. Walking into an enemy starts (or joins) a fight;
// walking onto a door or stairs takes you through.
func Move(s *world.State, p *world.Player, dir string) (string, error) {
	if err := canAct(s, p); err != nil {
		return "", err
	}
	if p.Battle != 0 {
		return "", oops("You're in a fight! Flee first.")
	}
	off, ok := world.Dirs[dir]
	if !ok {
		return "", oops("Unknown direction %q.", dir)
	}
	p.LastSeen = s.Now
	rm := s.Room(p.Room)
	nx, ny := p.X+off[0], p.Y+off[1]
	tile := rm.At(nx, ny)
	if tile == world.Wall {
		return "", nil // bumping a wall is free
	}
	if e := s.EnemyAt(rm.Num, nx, ny); e != nil {
		return engage(s, p, e), nil
	}
	if err := spend(s, p, world.StepCost); err != nil {
		return "", err
	}
	p.X, p.Y = nx, ny
	msg := ""
	switch tile {
	case world.Door:
		msg = goThrough(s, p, rm, world.DoorDir(nx, ny))
	case world.StairDown:
		msg = descend(s, p, rm)
	case world.StairUp:
		msg = ascend(s, p, rm)
	}
	if it := s.ItemAt(p.Room, p.X, p.Y); it != nil {
		it.Holder = p.User
		msg = fmt.Sprintf("You installed %s.", PackageName(s, it))
	}
	wander(s, p)
	return msg, nil
}

func goThrough(s *world.State, p *world.Player, rm *world.Room, dir string) string {
	off := world.Dirs[dir]
	next := s.RoomAt(rm.Floor, rm.X+off[0], rm.Y+off[1])
	if next == nil || !s.Linked(rm.Num, next.Num) {
		return ""
	}
	// Arrive one step inside the opposite door.
	dx, dy := world.DoorTile(world.Opposite(dir))
	p.Room, p.X, p.Y = next.Num, dx+off[0], dy+off[1]
	if e := s.EnemyAt(next.Num, p.X, p.Y); e != nil {
		e.X, e.Y = s.FreeTile(next) // shoo it out of the doorway
	}
	if !next.Revealed {
		next.Revealed = true
		if next.Kind == world.Stairs && next.Floor == world.Floors {
			s.Emit(p.User, "lair", fmt.Sprintf("%s found the lair of %s.", p.User, world.BossName))
			return "A cold wind. Something enormous lives here."
		}
		return "Nobody has been here before."
	}
	return ""
}

func descend(s *world.State, p *world.Player, rm *world.Room) string {
	below := s.FindKind(rm.Floor+1, world.Entrance)
	ux, uy := below.Find(world.StairUp)
	p.Room = below.Num
	p.X, p.Y = s.NearTile(below, ux, uy)
	if !below.Revealed {
		below.Revealed = true
		s.Emit(p.User, "floor", fmt.Sprintf("%s is the first to reach floor %d.", p.User, below.Floor))
		return fmt.Sprintf("You climb down to floor %d. You're the first one here.", below.Floor)
	}
	return fmt.Sprintf("You climb down to floor %d.", below.Floor)
}

func ascend(s *world.State, p *world.Player, rm *world.Room) string {
	above := s.FindKind(rm.Floor-1, world.Stairs)
	dx, dy := above.Find(world.StairDown)
	p.Room = above.Num
	p.X, p.Y = s.NearTile(above, dx, dy)
	return fmt.Sprintf("You climb back up to floor %d.", above.Floor)
}

// wander lets idle enemies in the player's room shuffle about as they move.
func wander(s *world.State, p *world.Player) {
	rm := s.Room(p.Room)
	for _, e := range s.Enemies {
		if e.Room != rm.Num || e.Boss || len(s.Fighters(e.ID)) > 0 || s.Rng.IntN(4) != 0 {
			continue
		}
		dirs := []string{"up", "down", "left", "right"}
		off := world.Dirs[dirs[s.Rng.IntN(4)]]
		nx, ny := e.X+off[0], e.Y+off[1]
		if rm.At(nx, ny) == world.Floor && s.EnemyAt(rm.Num, nx, ny) == nil && !occupied(s, rm.Num, nx, ny) {
			e.X, e.Y = nx, ny
		}
	}
}

func occupied(s *world.State, room, x, y int) bool {
	for _, o := range s.Players {
		if o.Room == room && o.X == x && o.Y == y {
			return true
		}
	}
	return false
}

// UseOutside uses a package while not fighting (patch or cache).
func UseOutside(s *world.State, p *world.Player, kind string) (string, error) {
	if err := canAct(s, p); err != nil {
		return "", err
	}
	it := s.Holding(p.User, kind)
	if it == nil {
		return "", oops("You don't have a %s package.", kind)
	}
	switch kind {
	case world.Patch:
		if p.HP >= p.MaxHP() {
			return "", oops("You're already at full health.")
		}
		heal(p)
	case world.Cache:
		p.Energy = min(world.MaxEnergy, p.Energy+25)
	default:
		return "", oops("%s only works in a fight.", kind)
	}
	p.LastSeen = s.Now
	s.RemoveItem(it)
	return useText(kind), nil
}

func heal(p *world.Player) { p.HP = min(p.MaxHP(), p.HP+p.MaxHP()/2) }

func useText(kind string) string {
	switch kind {
	case world.Patch:
		return "You apply the patch. Half your health comes back."
	case world.Cache:
		return "A warm cache hit. +25 energy."
	}
	return ""
}

// PackageName gives a package a stable npm-style name like "patch@2.4.1".
func PackageName(s *world.State, it *world.Item) string {
	h := s.Season.Seed*2654435761 + uint64(it.ID)*40503
	h ^= h >> 13
	return fmt.Sprintf("%s@%d.%d.%d", it.Kind, 1+h%4, (h>>8)%13, (h>>16)%20)
}
