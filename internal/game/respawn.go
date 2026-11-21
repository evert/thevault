package game

import (
	"time"

	"thevault/internal/world"
)

// RespawnAfter is how long a cleared room has to sit empty before new
// enemies move in.
const RespawnAfter = 24 * time.Hour

// BalanceVersion is bumped when enemy stats or numbers change, so Rebalance
// can bring a season that's already running up to date.
const BalanceVersion = 1

// Rebalance upgrades a running season to the current enemy rules: existing
// enemies get the new stats (keeping how hurt they are), and rooms nobody has
// cleared yet are topped up to the new numbers. Cleared rooms are left to
// Respawn. New seasons are generated with the current rules already.
func Rebalance(s *world.State) {
	if s.Balance >= BalanceVersion {
		return
	}
	for _, e := range s.Enemies {
		hp, atk := world.EnemyStats(s.Room(e.Room).Floor, e.Level, e.Boss)
		e.HP = max(1, e.HP*hp/e.MaxHP)
		e.MaxHP, e.Atk = hp, atk
	}
	for _, rm := range s.Rooms {
		n := enemiesIn(s, rm.Num)
		if !canSpawn(s, rm) || (rm.Revealed && n == 0) {
			continue
		}
		for range world.RoomEnemies(s.Rng) - n {
			addEnemy(s, rm)
		}
	}
	s.Balance = BalanceVersion
}

// Visit notes that every player's current room has someone in it right now.
func Visit(s *world.State) {
	for _, p := range s.Players {
		if rm := s.Room(p.Room); rm != nil {
			rm.Visited = s.Now
		}
	}
}

// Respawn refills explored rooms that have had no players and no enemies in
// them for RespawnAfter.
func Respawn(s *world.State) {
	Visit(s)
	for _, rm := range s.Rooms {
		if !rm.Revealed || !canSpawn(s, rm) || enemiesIn(s, rm.Num) > 0 {
			continue
		}
		if rm.Visited.IsZero() {
			rm.Visited = s.Now // from before rooms kept track: start the clock now
			continue
		}
		if s.Now.Sub(rm.Visited) < RespawnAfter {
			continue
		}
		for range world.RoomEnemies(s.Rng) {
			addEnemy(s, rm)
		}
		rm.Visited = s.Now // so a fresh clear starts a fresh wait
	}
}

// canSpawn is false for rooms that should never get new enemies: where
// players start or wake up, the boss's lair, and rooms someone is in.
func canSpawn(s *world.State, rm *world.Room) bool {
	switch {
	case rm == floorStart(s, rm.Floor):
		return false
	case rm.Floor == world.Floors && rm.Kind == world.Stairs:
		return false
	}
	return len(s.PlayersIn(rm.Num)) == 0
}

func enemiesIn(s *world.State, room int) int {
	n := 0
	for _, e := range s.Enemies {
		if e.Room == room {
			n++
		}
	}
	return n
}

func addEnemy(s *world.State, rm *world.Room) {
	e := world.NewEnemy(s.Rng, s.NextEnemyID(), rm.Floor)
	e.Room = rm.Num
	e.X, e.Y = s.FreeTile(rm)
	s.Enemies = append(s.Enemies, e)
}
