// Package render turns the world into terminal text.
package render

import (
	"fmt"
	"strings"
	"time"

	"thevault/internal/clock"
	"thevault/internal/game"
	"thevault/internal/world"
)

// Style adds ANSI colour when enabled.
type Style struct{ Color bool }

func (st Style) wrap(code, s string) string {
	if !st.Color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (st Style) Bold(s string) string   { return st.wrap("1", s) }
func (st Style) Dim(s string) string    { return st.wrap("2", s) }
func (st Style) Murk(s string) string   { return st.wrap("35", s) }
func (st Style) You(s string) string    { return st.wrap("1;33", s) }
func (st Style) Good(s string) string   { return st.wrap("32", s) }
func (st Style) Bad(s string) string    { return st.wrap("31", s) }
func (st Style) Accent(s string) string { return st.wrap("36", s) }

func Hearts(p *world.Player) string {
	return strings.Repeat("♥", p.HP) + strings.Repeat("♡", world.MaxHP-p.HP)
}

func depthName(depth int) string {
	return fmt.Sprintf("depth %d/%d", depth, world.Depths)
}

func roomTitle(rm *world.Room) string {
	switch rm.Kind {
	case world.Entrance:
		if rm.Depth == 1 {
			return "Camp"
		}
		return "Landing (stairs up)"
	case world.Stairs:
		if rm.Depth == world.Depths {
			return "The Deep Gate"
		}
		return "Stairwell (stairs down)"
	case world.Tarball:
		return "Tarball chamber"
	}
	return "Passage"
}

// Status is the default screen: you, your room, and what to do.
func Status(st Style, s *world.State, p *world.Player, notes []world.Note) string {
	var b strings.Builder
	rm := s.Room(p.Room)
	fmt.Fprintf(&b, "%s · season %d, day %d · next turns in %s\n",
		st.Bold("THE VAULT"), s.Season.ID, s.Day(), clock.Until(s.Now))
	state := ""
	if p.Downed {
		state = st.Bad(" · DOWNED")
	}
	fmt.Fprintf(&b, "%s  %s  turns %s  room %d, %s%s\n", st.You(p.User), st.Bad(Hearts(p)),
		st.Bold(fmt.Sprint(p.Turns)), rm.Num, depthName(rm.Depth), state)
	fmt.Fprintf(&b, "packages: %s\n\n", packages(s, p))
	b.WriteString(Look(st, s, p, notes))
	return b.String()
}

func packages(s *world.State, p *world.Player) string {
	var names []string
	for _, it := range s.Held(p.User) {
		names = append(names, game.PackageName(s, it))
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// Look describes the current room.
func Look(st Style, s *world.State, p *world.Player, notes []world.Note) string {
	var b strings.Builder
	rm := s.Room(p.Room)
	fmt.Fprintf(&b, "%s %s\n", st.Bold(fmt.Sprintf("Room %d:", rm.Num)), roomTitle(rm))
	if rm.Murk {
		b.WriteString(st.Murk("  The Murk swirls here. Anyone still here at 3 AM takes damage.") + "\n")
	}
	switch rm.Kind {
	case world.Tarball:
		if rm.BoxOpen {
			b.WriteString("  An unpacked tarball lies empty.\n")
		} else {
			fmt.Fprintf(&b, "  A sealed tarball, %d/2 keys turned. `open` it with two people, or `use key`.\n", rm.BoxKeys)
		}
	case world.Stairs:
		if rm.Depth == world.Depths {
			b.WriteString("  A vast gate. It wants the Lost Package. `descend` to pass.\n")
		} else {
			b.WriteString("  Stairs lead down. `descend` to go deeper.\n")
		}
	case world.Entrance:
		if rm.Depth > 1 {
			b.WriteString("  Stairs lead up. `ascend` to go back.\n")
		}
	}
	for _, it := range s.Items {
		if it.Hidden && it.Room == rm.Num {
			b.WriteString(st.Accent("  Something glints in the rubble. Maybe `search`?") + "\n")
			break
		}
	}
	for _, d := range s.Doors {
		if d.Plate == rm.Num && d.Kind == world.Plate && d.Revealed {
			fmt.Fprintf(&b, "  A pressure plate in the floor. Standing here holds open the door between %d and %d.\n", d.A, d.B)
		}
	}

	b.WriteString("  Exits:")
	any := false
	for _, dir := range []string{"n", "e", "s", "w"} {
		off := world.Dirs[dir]
		there := s.RoomAt(rm.Depth, rm.X+off[0], rm.Y+off[1])
		if there == nil {
			continue
		}
		d := s.DoorBetween(rm.Num, there.Num)
		if d == nil || !d.Revealed {
			continue
		}
		any = true
		label := fmt.Sprintf("%s→%d", dir, there.Num)
		switch d.Kind {
		case world.Locked:
			label += " (locked)"
		case world.Plate:
			label += fmt.Sprintf(" (sealed, plate in %d)", d.Plate)
		}
		if !there.Revealed {
			label += st.Dim(" unexplored")
		} else if there.Murk {
			label += st.Murk(" murky")
		}
		b.WriteString(" " + label)
	}
	if !any {
		b.WriteString(" none you can see")
	}
	b.WriteString("\n")

	var others []string
	for _, o := range s.PlayersIn(rm.Num) {
		if o == p {
			continue
		}
		name := o.User
		if o.Downed {
			name += st.Bad(" (downed)")
		}
		others = append(others, name)
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "  Here with you: %s\n", strings.Join(others, ", "))
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "  %s %s\n", st.Dim(fmt.Sprintf("note from %s, %s:", n.User, ago(s.Now, n.At))), n.Text)
	}
	return b.String()
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return "just now"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// Who lists everyone.
func Who(st Style, s *world.State, me *world.Player) string {
	var b strings.Builder
	for _, u := range s.Users() {
		p := s.Players[u]
		rm := s.Room(p.Room)
		name := fmt.Sprintf("%-12s", u)
		if p == me {
			name = st.You(name)
		}
		extra := ""
		if p.Downed {
			extra = st.Bad("  DOWNED")
		}
		fmt.Fprintf(&b, "%s %s  %d turns  room %-3d %s%s\n", name, st.Bad(Hearts(p)), p.Turns, rm.Num, depthName(rm.Depth), extra)
	}
	return b.String()
}

// Log shows recent events.
func Log(st Style, s *world.State, events []world.Event) string {
	if len(events) == 0 {
		return "Nothing has happened yet.\n"
	}
	var b strings.Builder
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		fmt.Fprintf(&b, "%s %s\n", st.Dim(e.At.In(clock.Zone).Format("Mon Jan 2 15:04")), e.Text)
	}
	return b.String()
}

const Help = `THE VAULT: a slow co-op dungeon. Everyone shares one world.
You get 3 turns at 3 AM ET (Toronto) each day, and can bank up to 6.

Costs a turn:
  go <n|s|e|w|room>   move to a neighbouring room (also: n, s, e, w)
  search              look for hidden packages and doors
  use key [dir]       unlock a door, or turn a key in a tarball's seal
  use torch           burn the Murk out of this room for two nights
  open                unseal a tarball (needs two people in the room)
  descend / ascend    take the stairs
  rest                recover one heart
  revive <user>       get a downed teammate back up

Free:
  (nothing)           your status and room
  map [depth]         the explored map
  look, who, log      room, players, recent happenings
  note "<text>"       scratch a note into the wall for others
  give <user> <pkg>   hand a package to someone in your room

Every night at 3 AM the Murk spreads. End your day in a murky room and you
lose a heart; at zero you're downed. A rope saves you once.
Goal: unpack the Lost Package on depth 5 and carry it through the Deep Gate.
`
