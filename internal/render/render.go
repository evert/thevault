// Package render draws the world: room tiles, the floor minimap, bars, and
// the plain-text outputs (MOTD, log, who). The TUI builds on these.
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"thevault/internal/clock"
	"thevault/internal/world"
)

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

var (
	WallStyle    = fg("238")
	FloorStyle   = fg("240")
	DoorStyle    = fg("130")
	StairStyle   = fg("255").Bold(true)
	EnemyStyle   = fg("197").Bold(true)
	PackageStyle = fg("220").Bold(true)
	KeyStyle     = fg("226").Bold(true) // tokens and the doors they open
	Dim          = fg("244")
	Bold         = lipgloss.NewStyle().Bold(true)
	HPStyle      = fg("203")
	XPStyle      = fg("141")
	EnergyStyle  = fg("214")
	Title        = fg("214").Bold(true)
)

// PlayerStyle colours a player's letter. Your own letter gets a solid
// background so you can always find yourself.
func PlayerStyle(p *world.Player, me bool) lipgloss.Style {
	st := lipgloss.NewStyle().Bold(true)
	if me {
		return st.Background(lipgloss.Color(fmt.Sprint(p.Color))).Foreground(lipgloss.Color("16"))
	}
	return st.Foreground(lipgloss.Color(fmt.Sprint(p.Color)))
}

func PlayerTag(p *world.Player, me bool) string {
	return PlayerStyle(p, me).Render(p.Letter())
}

// RoomLines draws a room, two terminal columns per tile. When the room is
// bigger than w x h tiles, it shows a window centred on (cx, cy).
func RoomLines(s *world.State, me *world.Player, rm *world.Room, cx, cy, w, h int) []string {
	w, h = min(w, world.RoomW), min(h, world.RoomH)
	x0 := clamp(cx-w/2, 0, world.RoomW-w)
	y0 := clamp(cy-h/2, 0, world.RoomH-h)

	people := map[[2]int]*world.Player{}
	for _, p := range s.PlayersIn(rm.Num) {
		k := [2]int{p.X, p.Y}
		if people[k] == nil || p == me {
			people[k] = p
		}
	}
	lines := make([]string, 0, h)
	for y := y0; y < y0+h; y++ {
		var b strings.Builder
		for x := x0; x < x0+w; x++ {
			b.WriteString(tile(s, me, rm, people, x, y))
		}
		lines = append(lines, b.String())
	}
	return lines
}

func tile(s *world.State, me *world.Player, rm *world.Room, people map[[2]int]*world.Player, x, y int) string {
	if p := people[[2]int{x, y}]; p != nil {
		return PlayerTag(p, p == me) + " "
	}
	if e := s.EnemyAt(rm.Num, x, y); e != nil {
		return EnemyTile(e)
	}
	if it := s.ItemAt(rm.Num, x, y); it != nil {
		if world.IsToken(it.Kind) {
			return KeyStyle.Render("⚷") + " "
		}
		return PackageStyle.Render("*") + " "
	}
	switch rm.At(x, y) {
	case world.Wall:
		return WallStyle.Render("██")
	case world.Door:
		if s.LockedDoor(rm, x, y) {
			return KeyStyle.Render("▓▓")
		}
		return DoorStyle.Render("░░")
	case world.StairDown:
		return StairStyle.Render(">") + " "
	case world.StairUp:
		return StairStyle.Render("<") + " "
	}
	return FloorStyle.Render("·") + " "
}

// EnemyTile shows an enemy's type icon. Its name and level only appear in a
// fight.
func EnemyTile(e *world.Enemy) string { return EnemyGlyph(e) + " " }

func EnemyGlyph(e *world.Enemy) string {
	t := world.TypeOf(e.Name)
	return fg(fmt.Sprint(t.Color)).Bold(true).Render(t.Glyph)
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

// Minimap draws a floor's explored rooms: one character per room, with
// links between them. Players show as their letter.
func Minimap(s *world.State, me *world.Player, floor int) []string {
	var lines []string
	for y := 0; y < world.FloorH; y++ {
		var row, below strings.Builder
		for x := 0; x < world.FloorW; x++ {
			rm := s.RoomAt(floor, x, y)
			row.WriteString(mapCell(s, me, rm))
			if x < world.FloorW-1 {
				east := s.RoomAt(floor, x+1, y)
				row.WriteString(link(s, rm, east, "─"))
			}
			south := s.RoomAt(floor, x, y+1)
			below.WriteString(link(s, rm, south, "│"))
			if x < world.FloorW-1 {
				below.WriteString(" ")
			}
		}
		lines = append(lines, row.String())
		if y < world.FloorH-1 {
			lines = append(lines, below.String())
		}
	}
	return lines
}

func link(s *world.State, a, b *world.Room, glyph string) string {
	if a == nil || b == nil || !s.Linked(a.Num, b.Num) || !(a.Revealed || b.Revealed) {
		return " "
	}
	return Dim.Render(glyph)
}

func mapCell(s *world.State, me *world.Player, rm *world.Room) string {
	if rm == nil {
		return " "
	}
	if !rm.Revealed {
		// Show rooms you know a door leads to.
		for _, d := range s.Doors {
			if (d.A == rm.Num || d.B == rm.Num) && s.Room(d.Other(rm.Num)).Revealed {
				if rm.Locked() {
					return KeyStyle.Render("▓")
				}
				return Dim.Render("?")
			}
		}
		return " "
	}
	if me.Room == rm.Num {
		return PlayerTag(me, true)
	}
	for _, p := range s.PlayersIn(rm.Num) {
		return PlayerTag(p, false)
	}
	switch rm.Kind {
	case world.Stairs:
		return StairStyle.Render(">")
	case world.Entrance:
		return StairStyle.Render("<")
	}
	return FloorStyle.Render("■")
}

// Bar draws a meter like ████░░░░.
func Bar(cur, max, width int, st lipgloss.Style) string {
	if max <= 0 {
		max = 1
	}
	n := clamp(cur*width/max, 0, width)
	if cur > 0 && n == 0 {
		n = 1
	}
	return st.Render(strings.Repeat("█", n)) + Dim.Render(strings.Repeat("░", width-n))
}

// How newsworthy each event kind is, for the MOTD headline.
var priority = map[string]int{
	"won": 100, "boss": 90, "lair": 80, "floor": 70, "unlock": 66, "key": 62, "ko": 60, "level": 55,
	"kill": 50, "wake": 30, "join": 20, "season": 15,
}

// Headline picks the most interesting recent event (events newest first;
// the newest wins ties). It also returns how many others there were.
func Headline(events []world.Event) (string, int) {
	best, bestP := "", -1
	for _, e := range events {
		if p := priority[e.Kind]; p > bestP {
			best, bestP = e.Text, p
		}
	}
	if best == "" {
		return "The Vault is quiet. Too quiet.", 0
	}
	return best, len(events) - 1
}

// Motd shows the day's headline, your room, and your stats.
func Motd(s *world.State, me *world.Player, events []world.Event) string {
	var b strings.Builder
	head, more := Headline(events)
	extra := ""
	if more > 0 {
		extra = Dim.Render(fmt.Sprintf("  (+%d more: thevault log)", more))
	}
	fmt.Fprintf(&b, "  %s %s %s%s\n", Title.Render("THE VAULT"), Dim.Render(fmt.Sprintf("day %d ·", s.Day())), head, extra)

	rm := s.Room(me.Room)
	room := RoomLines(s, me, rm, me.X, me.Y, world.RoomW, world.RoomH)
	side := []string{
		fmt.Sprintf("%s  %s", PlayerTag(me, true), Bold.Render(me.User)),
		fmt.Sprintf("Lv %d · floor %d/%d", me.Level, rm.Floor, world.Floors),
		HPStyle.Render("❤️ ") + Bar(me.HP, me.MaxHP(), 10, HPStyle) + fmt.Sprintf(" %d/%d", me.HP, me.MaxHP()),
		EnergyStyle.Render("⚡") + " " + Bar(me.Energy, world.MaxEnergy, 10, EnergyStyle) + fmt.Sprintf(" %d", me.Energy),
		"",
	}
	switch {
	case s.Won():
		side = append(side, "Season won by "+s.Season.Winner+"!", "New season at 3 AM ET.")
	case me.KO:
		side = append(side, HPStyle.Render("Knocked out."), "Back at 3 AM ET.")
	case me.Battle != 0:
		side = append(side, EnemyStyle.Render("You're mid-fight!"), "Type "+Bold.Render("thevault")+" to play.")
	default:
		side = append(side, "Type "+Bold.Render("thevault")+" to play.")
	}
	for _, p := range s.PlayersIn(rm.Num) {
		if p != me {
			side = append(side, "", Dim.Render("Here: ")+PlayerTag(p, false)+" "+p.User)
			break
		}
	}
	for i, line := range room {
		b.WriteString("  " + line)
		if i < len(side) {
			b.WriteString("   " + side[i])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Who lists everyone.
func Who(s *world.State, me *world.Player) string {
	var b strings.Builder
	for _, u := range s.Users() {
		p := s.Players[u]
		state := ""
		switch {
		case p.KO:
			state = HPStyle.Render("  knocked out")
		case p.Battle != 0:
			state = EnemyStyle.Render("  fighting")
		}
		fmt.Fprintf(&b, "%s %-12s Lv %-2d ❤️ %3d/%-3d ⚡ %3d  floor %d, room %d%s\n", PlayerTag(p, p == me), u,
			p.Level, p.HP, p.MaxHP(), p.Energy, s.Room(p.Room).Floor, p.Room, state)
	}
	return b.String()
}

// Log lists events, oldest first (events must be newest first).
func Log(events []world.Event) string {
	if len(events) == 0 {
		return "Nothing has happened yet.\n"
	}
	var b strings.Builder
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		fmt.Fprintf(&b, "%s %s\n", Dim.Render(e.At.In(clock.Zone).Format("Mon Jan 2 15:04")), e.Text)
	}
	return b.String()
}

// Ago formats how long ago something happened.
func Ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

const Help = `THE VAULT: a shared co-op dungeon crawler.

  thevault          play (arrow keys, hjkl or wasd to move)
  thevault motd     today's news and your room
  thevault who      everyone's stats
  thevault log      recent happenings
  thevault help     this text

You get 100 energy at 3 AM ET (Toronto) each day, banked up to 200.
A step costs 1. Walk into an enemy (a lowercase letter) to fight it. Attacking costs 3,
defending 2, using a package 1, and fleeing is free. Anyone who walks into
the same enemy joins the fight, and every ally adds +25% damage. Everyone
who lands a hit gets the XP.

Packages (*): patch heals, rimraf blasts an enemy, cache gives energy.
At 0 HP you're knocked out until 3 AM. Beat the boss on floor 5 to win.

The stairs on floors 1-4 are behind a locked door (yellow). Each of those
floors has an access token (⚷) lying in a room or carried by a monster:
vlt_1, vlt_2, vlt_3 and vlt_op. Walk into the door holding it to open the
way down for everyone. Get knocked out, or stay away for two days, and you
drop it.
`
