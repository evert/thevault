package render

import (
	"fmt"
	"strings"

	"thevault/internal/world"
)

// How newsworthy each event kind is, for picking the MOTD headline.
var priority = map[string]int{
	"won": 100, "relic": 90, "depth": 70, "downed": 65,
	"tarball_open": 55, "revive": 50, "stairs": 45, "tarball": 40,
	"unlock": 35, "rescued": 33, "torch": 30, "rope": 28, "found": 25,
	"hurt": 22, "join": 15, "season": 12, "murk": 10, "note": 8, "give": 5,
}

// Headline picks the most interesting recent event (newest wins ties).
// events must be newest first.
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

// Motd is at most five short lines, shown at every login.
func Motd(st Style, s *world.State, p *world.Player, events []world.Event) string {
	var b strings.Builder
	head, more := Headline(events)
	extra := ""
	if more > 0 {
		extra = st.Dim(fmt.Sprintf("  (+%d more: thevault log)", more))
	}
	fmt.Fprintf(&b, "%s %s %s%s\n", st.Bold("THE VAULT"), st.Dim(fmt.Sprintf("day %d ·", s.Day())), head, extra)

	rm := s.Room(p.Room)
	depth := rm.Depth
	explored, total, murky, here := 0, 0, 0, 0
	for _, r := range s.DepthRooms(depth) {
		total++
		if r.Revealed {
			explored++
			if r.Murk {
				murky++
			}
		}
	}
	for _, o := range s.Players {
		if s.Room(o.Room).Depth == depth {
			here++
		}
	}
	relic := "the Lost Package is still sealed away"
	for _, it := range s.Items {
		if it.Kind == world.Relic && it.Holder != "" {
			relic = it.Holder + " holds the Lost Package"
		}
	}
	if s.Won() {
		relic = "season won by " + s.Season.Winner
	}
	info := []string{
		fmt.Sprintf("%s · %d/%d rooms explored", depthName(depth), explored, total),
		fmt.Sprintf("murk in %d explored rooms · %d explorer(s) here", murky, here),
		relic,
	}
	for i, line := range compact(st, s, p, depth) {
		fmt.Fprintf(&b, "  %s   %s\n", line, st.Dim(info[i]))
	}

	switch {
	case s.Won():
		b.WriteString("  A new season starts at 3 AM ET.\n")
	case p.Downed:
		fmt.Fprintf(&b, "  %s you're downed in room %d. Ask for a revive.\n", st.Bad(p.User+":"), p.Room)
	default:
		fmt.Fprintf(&b, "  %s %s · %s · room %d · type %s to play\n", st.You(p.User+":"), st.Bad(Hearts(p)),
			st.Bold(fmt.Sprintf("%d turns", p.Turns)), p.Room, st.Accent("thevault"))
	}
	return b.String()
}
