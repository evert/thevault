// Package game holds the rules: the daily tick and the player actions.
package game

import (
	"fmt"
	"math/rand/v2"
	"time"

	"thevault/internal/clock"
	"thevault/internal/world"
)

// MaxCatchUp limits how many missed days are replayed at once. Turns cap at
// world.MaxTurns anyway, so replaying more would only spread more Murk.
const MaxCatchUp = 7

// Start creates the very first world.
func Start(s *world.State, seed uint64) {
	s.Tick = 1
	s.LastTick = clock.LatestBoundary(s.Now)
	world.NewSeason(s, 1, seed)
	s.Emit("", "season", "Season 1 begins. The Vault is open.")
}

// CatchUp runs a tick for every 3 AM boundary passed since the last one.
// It returns how many ticks ran.
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
	r := rand.New(rand.NewPCG(s.Season.Seed, uint64(s.Tick)))

	if s.Won() {
		id := s.Season.ID + 1
		world.NewSeason(s, id, newSeed())
		s.Emit("", "season", fmt.Sprintf("Season %d begins. Everyone is back at camp.", id))
		refill(s)
		return
	}

	// Anyone downed for a full day is dragged back to their depth's entrance.
	for _, u := range s.Users() {
		p := s.Players[u]
		if !p.Downed {
			continue
		}
		ent := s.FindKind(s.Room(p.Room).Depth, world.Entrance)
		p.Room, p.HP, p.Downed = ent.Num, world.MaxHP, false
		s.Emit(u, "rescued", fmt.Sprintf("%s was dragged back to room %d, bruised but alive.", u, ent.Num))
	}

	spreadMurk(s, r)

	for _, u := range s.Users() {
		p := s.Players[u]
		if !s.Room(p.Room).Murk {
			continue
		}
		if rope := s.Holding(u, world.Rope); rope != nil {
			rope.Holder = ""
			s.Emit(u, "rope", fmt.Sprintf("%s tied off a rope and rode out the Murk in room %d.", u, p.Room))
			continue
		}
		p.HP--
		if p.HP <= 0 {
			p.HP, p.Downed = 0, true
			s.Emit(u, "downed", fmt.Sprintf("%s was downed by the Murk in room %d. Someone revive them!", u, p.Room))
		} else {
			s.Emit(u, "hurt", fmt.Sprintf("%s choked on the Murk in room %d.", u, p.Room))
		}
	}

	refill(s)
}

func refill(s *world.State) {
	for _, p := range s.Players {
		p.Turns = min(p.Turns+world.TurnsPerTick, world.MaxTurns)
	}
}

// spreadMurk grows the Murk by one room per source, but only on depths
// someone has reached: it stirs when disturbed.
func spreadMurk(s *world.State, r *rand.Rand) {
	reached := map[int]bool{}
	for _, rm := range s.Rooms {
		if rm.Revealed {
			reached[rm.Depth] = true
		}
	}
	spread := 0
	var seen []int
	for _, src := range s.Rooms {
		if !src.Source || !reached[src.Depth] {
			continue
		}
		if !src.Murk {
			if src.SafeUntil < s.Tick {
				src.Murk = true
				if src.Revealed {
					spread++
					seen = append(seen, src.Num)
				}
			}
			continue
		}
		// Find murky rooms connected to this source, then the clean rooms next to them.
		region := map[int]bool{src.Num: true}
		queue := []int{src.Num}
		var edge []int
		edgeSeen := map[int]bool{}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, d := range s.DoorsOf(cur) {
				o := s.Room(d.Other(cur))
				switch {
				case region[o.Num] || edgeSeen[o.Num]:
				case o.Murk:
					region[o.Num] = true
					queue = append(queue, o.Num)
				case o.SafeUntil < s.Tick && o != s.Camp():
					edgeSeen[o.Num] = true
					edge = append(edge, o.Num)
				}
			}
		}
		if len(edge) == 0 {
			continue
		}
		o := s.Room(edge[r.IntN(len(edge))])
		o.Murk = true
		if o.Revealed {
			spread++
			seen = append(seen, o.Num)
		}
	}
	switch {
	case spread == 1:
		s.Emit("", "murk", fmt.Sprintf("The Murk crept into room %d overnight.", seen[0]))
	case spread > 1:
		s.Emit("", "murk", fmt.Sprintf("The Murk crept into %d explored rooms overnight.", spread))
	}
}

// NewSeed returns a seed for a new season.
func NewSeed() uint64 { return uint64(time.Now().UnixNano()) }
