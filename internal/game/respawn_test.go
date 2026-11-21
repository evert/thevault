package game

import (
	"testing"
	"time"

	"thevault/internal/world"
)

// emptyRoom returns an explored room on floor 2 that's allowed to refill,
// cleared of enemies.
func emptyRoom(t *testing.T, s *world.State) *world.Room {
	t.Helper()
	for _, rm := range s.FloorRooms(2) {
		if canSpawn(s, rm) {
			rm.Revealed = true
			s.Enemies = slicesWithout(s.Enemies, rm.Num)
			return rm
		}
	}
	t.Fatal("no room can spawn")
	return nil
}

func slicesWithout(es []*world.Enemy, room int) []*world.Enemy {
	var out []*world.Enemy
	for _, e := range es {
		if e.Room != room {
			out = append(out, e)
		}
	}
	return out
}

func TestClearedRoomRefillsAfterADay(t *testing.T) {
	s := newState(5)
	Join(s, "alice")
	rm := emptyRoom(t, s)
	rm.Visited = s.Now

	s.Now = s.Now.Add(RespawnAfter - 1)
	Respawn(s)
	if enemiesIn(s, rm.Num) != 0 {
		t.Fatal("room refilled too soon")
	}
	s.Now = s.Now.Add(1)
	Respawn(s)
	if n := enemiesIn(s, rm.Num); n < 1 || n > 2 {
		t.Fatalf("room refilled with %d enemies, want 1-2", n)
	}
}

func TestOccupiedRoomDoesntRefill(t *testing.T) {
	s := newState(5)
	p := Join(s, "alice")
	rm := emptyRoom(t, s)
	rm.Visited = s.Now
	p.Room = rm.Num
	s.Now = s.Now.Add(3 * RespawnAfter)
	Respawn(s)
	if enemiesIn(s, rm.Num) != 0 {
		t.Fatal("enemies appeared in a room with a player in it")
	}
	if !rm.Visited.Equal(s.Now) {
		t.Fatal("standing in a room didn't count as a visit")
	}
}

func TestRoomsFromOldSavesStartTheirClock(t *testing.T) {
	s := newState(5)
	Join(s, "alice")
	rm := emptyRoom(t, s)
	rm.Visited = time.Time{} // saved before visits were tracked
	Respawn(s)
	if enemiesIn(s, rm.Num) != 0 || !rm.Visited.Equal(s.Now) {
		t.Fatal("an old cleared room refilled straight away")
	}
}

func TestRebalanceUpgradesARunningSeason(t *testing.T) {
	s := newState(5)
	Join(s, "alice")
	s.Balance = 0
	var hurt *world.Enemy
	for _, e := range s.Enemies {
		if s.Room(e.Room).Floor == 3 {
			// Pretend it was made with the old, weaker stats and is half dead.
			e.MaxHP, e.Atk = 8+6*e.Level, 1+3*e.Level/2
			e.HP = e.MaxHP / 2
			hurt = e
			break
		}
	}
	cleared := emptyRoom(t, s)
	Rebalance(s)
	if s.Balance != BalanceVersion {
		t.Fatal("balance version not updated")
	}
	hp, atk := world.EnemyStats(3, hurt.Level, false)
	if hurt.MaxHP != hp || hurt.Atk != atk || hurt.HP != hp/2 {
		t.Fatalf("enemy stats: hp %d/%d atk %d, want %d/%d atk %d", hurt.HP, hurt.MaxHP, hurt.Atk, hp/2, hp, atk)
	}
	if enemiesIn(s, cleared.Num) != 0 {
		t.Fatal("a cleared room was refilled by the upgrade")
	}
	for _, rm := range s.Rooms {
		if canSpawn(s, rm) && rm != cleared && enemiesIn(s, rm.Num) == 0 {
			t.Fatalf("room %d still has no enemies", rm.Num)
		}
	}
	n := len(s.Enemies)
	Rebalance(s)
	if len(s.Enemies) != n {
		t.Fatal("rebalance ran twice")
	}
}

func TestDeeperFloorsAreHarder(t *testing.T) {
	hp1, atk1 := world.EnemyStats(1, 3, false)
	if hp1 != 8+6*3 || atk1 != 1+3*3/2 {
		t.Fatal("floor 1 stats changed")
	}
	for floor := 2; floor <= world.Floors; floor++ {
		hp, atk := world.EnemyStats(floor, 3, false)
		if hp <= hp1 || atk <= atk1 {
			t.Fatalf("floor %d isn't harder: hp %d atk %d", floor, hp, atk)
		}
	}
}
