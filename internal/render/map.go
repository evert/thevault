package render

import (
	"fmt"
	"strings"

	"thevault/internal/world"
)

// frontier reports whether an unexplored room has a known door into it.
func frontier(s *world.State, rm *world.Room) bool {
	for _, d := range s.DoorsOf(rm.Num) {
		if d.Revealed {
			return true
		}
	}
	return false
}

func occupants(s *world.State, rm *world.Room, me *world.Player) (you, others, downed bool) {
	for _, p := range s.PlayersIn(rm.Num) {
		switch {
		case p == me:
			you = true
		case p.Downed:
			downed = true
		default:
			others = true
		}
	}
	return
}

func doorMark(d *world.Door, open, locked, plate string) string {
	switch d.Kind {
	case world.Locked:
		return locked
	case world.Plate:
		return plate
	}
	return open
}

// Map draws one depth as a grid of rooms like [07]──[08].
func Map(st Style, s *world.State, me *world.Player, depth int) string {
	var b strings.Builder
	explored, total := 0, 0
	for _, rm := range s.DepthRooms(depth) {
		total++
		if rm.Revealed {
			explored++
		}
	}
	if explored == 0 {
		return fmt.Sprintf("Nobody has reached depth %d yet.\n", depth)
	}
	fmt.Fprintf(&b, "%s · %d of %d rooms explored\n\n", st.Bold(strings.ToUpper(depthName(depth))), explored, total)

	for y := 0; y < world.GridH; y++ {
		var row, below strings.Builder
		row.WriteString("  ")
		below.WriteString("  ")
		for x := 0; x < world.GridW; x++ {
			rm := s.RoomAt(depth, x, y)
			row.WriteString(cell(st, s, rm, me))
			// Door to the east.
			conn := "   "
			if east := s.RoomAt(depth, x+1, y); rm != nil && east != nil {
				if d := s.DoorBetween(rm.Num, east.Num); d != nil && d.Revealed {
					conn = doorMark(d, "───", "─#─", "─P─")
				}
			}
			if x < world.GridW-1 {
				row.WriteString(conn)
			}
			// Door to the south.
			v := " "
			if south := s.RoomAt(depth, x, y+1); rm != nil && south != nil {
				if d := s.DoorBetween(rm.Num, south.Num); d != nil && d.Revealed {
					v = doorMark(d, "│", "#", "P")
				}
			}
			below.WriteString(" " + v + "  ")
			if x < world.GridW-1 {
				below.WriteString("   ")
			}
		}
		b.WriteString(strings.TrimRight(row.String(), " ") + "\n")
		if y < world.GridH-1 {
			b.WriteString(strings.TrimRight(below.String(), " ") + "\n")
		}
	}
	b.WriteString("\n" + st.Dim("  @ you  * others  x downed  > down  < up  ^ camp  $ tarball  ~ murk") + "\n")
	b.WriteString(st.Dim("  ?? unexplored  # locked door  P plate door") + "\n")
	return b.String()
}

func cell(st Style, s *world.State, rm *world.Room, me *world.Player) string {
	if rm == nil {
		return "    "
	}
	if !rm.Revealed {
		if frontier(s, rm) {
			return st.Dim(" ?? ")
		}
		return "    "
	}
	left, right := "[", "]"
	you, others, downed := occupants(s, rm, me)
	switch {
	case you:
		left = "@"
	case others:
		left = "*"
	case downed:
		left = "x"
	}
	switch {
	case rm.Kind == world.Stairs:
		right = ">"
	case rm.Kind == world.Entrance && rm.Depth == 1:
		right = "^"
	case rm.Kind == world.Entrance:
		right = "<"
	case rm.Kind == world.Tarball && !rm.BoxOpen:
		right = "$"
	case rm.Murk:
		right = "~"
	}
	text := fmt.Sprintf("%s%02d%s", left, rm.Num, right)
	switch {
	case you:
		return st.You(text)
	case rm.Murk:
		return st.Murk(text)
	}
	return text
}

// compact draws a depth as three short lines for the MOTD, one character per room.
func compact(st Style, s *world.State, me *world.Player, depth int) []string {
	lines := make([]string, world.GridH)
	for y := 0; y < world.GridH; y++ {
		var b strings.Builder
		for x := 0; x < world.GridW; x++ {
			rm := s.RoomAt(depth, x, y)
			b.WriteString(dot(st, s, rm, me))
			conn := " "
			if east := s.RoomAt(depth, x+1, y); rm != nil && east != nil {
				if d := s.DoorBetween(rm.Num, east.Num); d != nil && d.Revealed {
					conn = doorMark(d, "─", "#", "P")
				}
			}
			if x < world.GridW-1 {
				b.WriteString(conn)
			}
		}
		lines[y] = b.String()
	}
	return lines
}

func dot(st Style, s *world.State, rm *world.Room, me *world.Player) string {
	if rm == nil {
		return " "
	}
	if !rm.Revealed {
		if frontier(s, rm) {
			return st.Dim("?")
		}
		return " "
	}
	you, others, _ := occupants(s, rm, me)
	switch {
	case you:
		return st.You("@")
	case others:
		return st.Bold("*")
	case rm.Kind == world.Stairs:
		return ">"
	case rm.Kind == world.Tarball && !rm.BoxOpen:
		return "$"
	case rm.Murk:
		return st.Murk("~")
	}
	return "·"
}
