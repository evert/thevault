// Package world holds the game's data model and the dungeon generator.
package world

import (
	"maps"
	"slices"
	"time"
)

const (
	GridW        = 7 // rooms per depth live on a GridW x GridH grid
	GridH        = 3
	Depths       = 5
	MaxHP        = 3
	TurnsPerTick = 3
	MaxTurns     = 6
	NoteMaxLen   = 140
)

// Room kinds.
const (
	Plain    = ""
	Entrance = "entrance" // stairs up; on depth 1 this is camp
	Stairs   = "stairs"   // stairs down; on the last depth, the Deep Gate
	Tarball  = "tarball"  // sealed; opened with two keys or two people
)

// Door kinds.
const (
	Open   = "open"
	Locked = "locked"
	Plate  = "plate" // passable only while someone else stands in the plate room
)

// Item kinds.
const (
	Key   = "key"
	Torch = "torch"
	Rope  = "rope"
	Relic = "relic" // shown as "the Lost Package"
)

type Room struct {
	Num       int // unique within a season, what players type
	Depth     int
	X, Y      int
	Kind      string
	Revealed  bool
	Murk      bool
	Source    bool // the Murk regrows from here
	SafeUntil int  // tick until which the Murk can't enter (torch)
	BoxOpen   bool
	BoxKeys   int
}

type Door struct {
	A, B     int // room numbers, A < B
	Kind     string
	Hidden   bool // needs a search to find
	Revealed bool // shown on the map
	Plate    int  // room number holding the plate, for Plate doors
}

// Other returns the room on the far side of the door from num.
func (d *Door) Other(num int) int {
	if d.A == num {
		return d.B
	}
	return d.A
}

type Item struct {
	ID     int // unique within a season
	Kind   string
	Room   int    // room it lies in (hidden, or inside the strongbox)
	Holder string // player carrying it
	Hidden bool
	InBox  bool
}

// Gone reports whether the item was used up.
func (i *Item) Gone() bool { return i.Room == 0 && i.Holder == "" }

type Player struct {
	User   string
	Room   int
	HP     int
	Turns  int
	Downed bool
	Joined time.Time
}

type Season struct {
	ID        int
	Seed      uint64
	StartTick int
	StartedAt time.Time
	WonTick   int // 0 while in progress
	Winner    string
}

type Event struct {
	Tick int
	At   time.Time
	User string
	Kind string
	Text string
}

type Note struct {
	Room int
	User string
	Text string
	At   time.Time
}

// State is everything a command needs. It is loaded, changed in memory and
// saved back inside one database transaction.
type State struct {
	Season   Season
	Tick     int       // number of ticks since the world began
	LastTick time.Time // the boundary the last tick ran for
	Now      time.Time

	Rooms   []*Room // index = Num-1
	Doors   []*Door
	Items   []*Item
	Players map[string]*Player

	// Unsaved additions.
	Events     []Event
	Notes      []Note
	FreshWorld bool // season rows need inserting rather than updating
}

func (s *State) Room(num int) *Room {
	if num < 1 || num > len(s.Rooms) {
		return nil
	}
	return s.Rooms[num-1]
}

func (s *State) DoorsOf(num int) []*Door {
	var out []*Door
	for _, d := range s.Doors {
		if d.A == num || d.B == num {
			out = append(out, d)
		}
	}
	return out
}

// DoorBetween returns the door joining two rooms, or nil.
func (s *State) DoorBetween(a, b int) *Door {
	if a > b {
		a, b = b, a
	}
	for _, d := range s.Doors {
		if d.A == a && d.B == b {
			return d
		}
	}
	return nil
}

// RoomAt returns the room at grid cell (x, y) on a depth, or nil.
func (s *State) RoomAt(depth, x, y int) *Room {
	for _, r := range s.Rooms {
		if r.Depth == depth && r.X == x && r.Y == y {
			return r
		}
	}
	return nil
}

// FindKind returns the first room of a kind on a depth.
func (s *State) FindKind(depth int, kind string) *Room {
	for _, r := range s.Rooms {
		if r.Depth == depth && r.Kind == kind {
			return r
		}
	}
	return nil
}

func (s *State) Camp() *Room { return s.FindKind(1, Entrance) }

// PlayersIn returns the players in a room, sorted by name.
func (s *State) PlayersIn(num int) []*Player {
	var out []*Player
	for _, u := range s.Users() {
		if p := s.Players[u]; p.Room == num {
			out = append(out, p)
		}
	}
	return out
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

// Users returns player names in sorted order.
func (s *State) Users() []string {
	return slices.Sorted(maps.Keys(s.Players))
}

// Day is the day number within the current season, starting at 1.
func (s *State) Day() int { return s.Tick - s.Season.StartTick + 1 }

func (s *State) Won() bool { return s.Season.WonTick != 0 }

func (s *State) Emit(user, kind, text string) {
	s.Events = append(s.Events, Event{Tick: s.Tick, At: s.Now, User: user, Kind: kind, Text: text})
}

// Direction offsets on the grid.
var Dirs = map[string][2]int{
	"n": {0, -1}, "s": {0, 1}, "e": {1, 0}, "w": {-1, 0},
}

var DirNames = map[string]string{"n": "north", "s": "south", "e": "east", "w": "west"}

// DirTo returns the direction letter from room a to adjacent room b.
func DirTo(a, b *Room) string {
	for k, v := range Dirs {
		if a.X+v[0] == b.X && a.Y+v[1] == b.Y {
			return k
		}
	}
	return "?"
}
