// Package world holds the game's data model and the dungeon generator.
package world

import (
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	FloorW = 5 // each floor is a FloorW x FloorH grid of rooms
	FloorH = 3
	Floors = 5
	RoomW  = 21 // each room is a RoomW x RoomH grid of tiles, walls included
	RoomH  = 9

	EnergyPerTick = 100
	MaxEnergy     = 200
	StepCost      = 1
	AttackCost    = 3
	DefendCost    = 2
	PackageCost   = 1

	AllyBonus = 0.25 // extra damage per other player in the same fight
)

// Tiles.
const (
	Wall      = '#'
	Floor     = '.'
	Door      = 'D'
	StairDown = '>'
	StairUp   = '<'
)

// Room kinds.
const (
	Plain    = ""
	Entrance = "entrance" // stairs up (none on floor 1)
	Stairs   = "stairs"   // stairs down; on the last floor, the boss's lair
)

// Package kinds.
const (
	Patch  = "patch"  // heal half your HP
	Rimraf = "rimraf" // blast an enemy
	Cache  = "cache"  // restore energy
)

var PackageKinds = []string{Patch, Rimraf, Cache}

// Tokens open the locked stairs: vlt_1 on floor 1 gets you to floor 2, and
// so on, with vlt_op opening the way to the last floor. Each floor with
// stairs has its token lying in a room or carried by a monster.
var tokens = map[int]string{1: "vlt_1", 2: "vlt_2", 3: "vlt_3", 4: "vlt_op"}

// Token names the token that unlocks a floor's stairs ("" if none).
func Token(floor int) string { return tokens[floor] }

// TokenFloor is the floor a token belongs to, or 0 for other items.
func TokenFloor(kind string) int {
	for f, t := range tokens {
		if t == kind {
			return f
		}
	}
	return 0
}

func IsToken(kind string) bool { return TokenFloor(kind) != 0 }

type Room struct {
	Num      int // unique within a season
	Floor    int
	X, Y     int // position in the floor grid
	Kind     string
	Revealed bool
	Unlocked bool               // a stairs room stays locked until someone uses its token
	Visited  time.Time          // last time a player was seen in it; zero if never
	Tiles    [RoomH][RoomW]byte // generated from the seed, never stored
}

// Locked is true for a stairs room nobody has opened yet. Every door into it
// is locked. The boss's lair on the last floor has no lock.
func (r *Room) Locked() bool { return r.Kind == Stairs && Token(r.Floor) != "" && !r.Unlocked }

func (r *Room) At(x, y int) byte {
	if x < 0 || y < 0 || x >= RoomW || y >= RoomH {
		return Wall
	}
	return r.Tiles[y][x]
}

// Find returns the first tile of a type, or (-1, -1).
func (r *Room) Find(t byte) (int, int) {
	for y := range RoomH {
		for x := range RoomW {
			if r.Tiles[y][x] == t {
				return x, y
			}
		}
	}
	return -1, -1
}

type DoorLink struct{ A, B int } // two adjacent rooms, A < B

func (d DoorLink) Other(num int) int {
	if d.A == num {
		return d.B
	}
	return d.A
}

type Enemy struct {
	ID      int
	Room    int
	X, Y    int
	Name    string
	Level   int
	HP      int
	MaxHP   int
	Atk     int
	Boss    bool
	Key     bool            // carries its floor's token, dropped when it dies
	Hitters map[string]bool // everyone who landed a hit gets the XP
}

type Item struct {
	ID     int
	Kind   string
	Room   int // where it lies, when nobody holds it
	X, Y   int
	Holder string
}

type Player struct {
	User     string
	Color    int // 256-colour palette index
	Room     int
	X, Y     int
	HP       int
	Level    int
	XP       int
	Energy   int
	Known    map[string]bool // enemy types you have fought
	Battle   int             // enemy ID while in a fight
	Braced   bool            // defended: the next attack hits 50% harder
	KO       bool            // knocked out until the next tick
	LastSeen time.Time
	Joined   time.Time
}

// Letter is the player's map icon: the first letter of their name, uppercased.
func (p *Player) Letter() string {
	for _, r := range p.User {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "?"
}

func MaxHPFor(level int) int { return 20 + 5*(level-1) }
func AtkFor(level int) int   { return 5 + 2*(level-1) }
func XPToNext(level int) int { return 10 * level }

func (p *Player) MaxHP() int { return MaxHPFor(p.Level) }
func (p *Player) Atk() int   { return AtkFor(p.Level) }

type Season struct {
	ID        int
	Seed      uint64
	StartTick int
	StartedAt time.Time
	WonTick   int // 0 while in progress
	Winner    string
}

type Event struct {
	Tick  int
	At    time.Time
	User  string
	Kind  string
	Text  string
	Enemy int // set for battle log lines
}

// State is everything a command needs. It is loaded, changed in memory and
// saved back inside one database transaction.
type State struct {
	Season   Season
	Tick     int       // number of ticks since the world began
	LastTick time.Time // the boundary the last tick ran for
	Now      time.Time
	Rng      *rand.Rand

	Rooms   []*Room // index = Num-1
	Doors   []DoorLink
	Enemies []*Enemy
	Items   []*Item
	Players map[string]*Player

	EnemySeq   int     // last enemy ID handed out this season
	Balance    int     // which game.BalanceVersion the world has been brought up to
	Events     []Event // unsaved
	FreshWorld bool    // the season row needs inserting
}

func (s *State) Room(num int) *Room {
	if num < 1 || num > len(s.Rooms) {
		return nil
	}
	return s.Rooms[num-1]
}

func (s *State) RoomAt(floor, x, y int) *Room {
	for _, r := range s.Rooms {
		if r.Floor == floor && r.X == x && r.Y == y {
			return r
		}
	}
	return nil
}

func (s *State) Linked(a, b int) bool {
	if a > b {
		a, b = b, a
	}
	return slices.Contains(s.Doors, DoorLink{a, b})
}

// Beyond returns the room a door tile leads to, or nil.
func (s *State) Beyond(rm *Room, x, y int) *Room {
	off, ok := Dirs[DoorDir(x, y)]
	if !ok {
		return nil
	}
	next := s.RoomAt(rm.Floor, rm.X+off[0], rm.Y+off[1])
	if next == nil || !s.Linked(rm.Num, next.Num) {
		return nil
	}
	return next
}

// LockedDoor is true when the door tile at (x, y) leads into a locked room.
// Doors only lock from the outside, so nobody can get shut in.
func (s *State) LockedDoor(rm *Room, x, y int) bool {
	if rm.At(x, y) != Door {
		return false
	}
	next := s.Beyond(rm, x, y)
	return next != nil && next.Locked()
}

func (s *State) FindKind(floor int, kind string) *Room {
	for _, r := range s.Rooms {
		if r.Floor == floor && r.Kind == kind {
			return r
		}
	}
	return nil
}

func (s *State) FloorRooms(floor int) []*Room {
	var out []*Room
	for _, r := range s.Rooms {
		if r.Floor == floor {
			out = append(out, r)
		}
	}
	return out
}

func (s *State) Enemy(id int) *Enemy {
	for _, e := range s.Enemies {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (s *State) EnemyAt(room, x, y int) *Enemy {
	for _, e := range s.Enemies {
		if e.Room == room && e.X == x && e.Y == y {
			return e
		}
	}
	return nil
}

func (s *State) ItemAt(room, x, y int) *Item {
	for _, it := range s.Items {
		if it.Holder == "" && it.Room == room && it.X == x && it.Y == y {
			return it
		}
	}
	return nil
}

func (s *State) Held(user string) []*Item {
	var out []*Item
	for _, it := range s.Items {
		if it.Holder == user {
			out = append(out, it)
		}
	}
	return out
}

func (s *State) Holding(user, kind string) *Item {
	for _, it := range s.Items {
		if it.Holder == user && it.Kind == kind {
			return it
		}
	}
	return nil
}

// RemoveItem deletes a used-up package.
func (s *State) RemoveItem(it *Item) {
	s.Items = slices.DeleteFunc(s.Items, func(o *Item) bool { return o == it })
}

func (s *State) RemoveEnemy(e *Enemy) {
	s.Enemies = slices.DeleteFunc(s.Enemies, func(o *Enemy) bool { return o == e })
}

// Fighters returns the conscious players fighting an enemy, sorted by name.
func (s *State) Fighters(enemy int) []*Player {
	var out []*Player
	for _, u := range s.Users() {
		if p := s.Players[u]; p.Battle == enemy && !p.KO {
			out = append(out, p)
		}
	}
	return out
}

func (s *State) PlayersIn(room int) []*Player {
	var out []*Player
	for _, u := range s.Users() {
		if p := s.Players[u]; p.Room == room {
			out = append(out, p)
		}
	}
	return out
}

func (s *State) Users() []string { return slices.Sorted(maps.Keys(s.Players)) }

// Day is the day number within the current season, starting at 1.
func (s *State) Day() int { return s.Tick - s.Season.StartTick + 1 }

func (s *State) Won() bool { return s.Season.WonTick != 0 }

// NextEnemyID never reuses an ID within a season, so fight logs stay separate.
func (s *State) NextEnemyID() int {
	s.EnemySeq++
	return s.EnemySeq
}

func (s *State) NextItemID() int {
	n := 0
	for _, it := range s.Items {
		n = max(n, it.ID)
	}
	return n + 1
}

func (s *State) Emit(user, kind, text string) {
	s.Events = append(s.Events, Event{Tick: s.Tick, At: s.Now, User: user, Kind: kind, Text: text})
}

// BattleLog adds a line to an enemy's fight log.
func (s *State) BattleLog(enemy int, user, text string) {
	s.Events = append(s.Events, Event{Tick: s.Tick, At: s.Now, User: user, Kind: "battle", Text: text, Enemy: enemy})
}

// Dirs maps direction names to grid offsets.
var Dirs = map[string][2]int{
	"up": {0, -1}, "down": {0, 1}, "left": {-1, 0}, "right": {1, 0},
}
