// Package tui is the full-screen game: walk with the arrow keys, fight on
// the battle screen, and watch teammates move live.
package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"thevault/internal/app"
	"thevault/internal/clock"
	"thevault/internal/game"
	"thevault/internal/render"
	"thevault/internal/store"
	"thevault/internal/world"
)

const (
	pollEvery  = 500 * time.Millisecond
	leftWidth  = world.RoomW*2 + 2
	sideWidth  = 32
	battleWait = 400 * time.Millisecond // ignore held keys right after a fight starts
)

var battleOptions = []struct{ label, cost string }{
	{"Attack", fmt.Sprintf("%d⚡", world.AttackCost)},
	{"Defend", fmt.Sprintf("%d⚡", world.DefendCost)},
	{"Package", fmt.Sprintf("%d⚡", world.PackageCost)},
	{"Flee", "free"},
}

type pollMsg struct{}

type Model struct {
	db   *store.DB
	user string

	s  *world.State
	me *world.Player

	msg       string
	msgBad    bool
	news      []world.Event // recent world events, newest first
	fight     []world.Event // current fight's log, newest first
	lastFight int           // enemy we were fighting at the last refresh
	fightFrom time.Time
	menu      int
	pkgMenu   bool
	pkgCursor int
	help      bool
	quit      bool
	err       error
}

func New(db *store.DB, user string) (*Model, error) {
	m := &Model{db: db, user: user}
	m.act(nil)
	return m, m.err
}

func Run(db *store.DB, user string) error {
	m, err := New(db, user)
	if err != nil {
		return err
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	return final.(*Model).err
}

func poll() tea.Cmd { return tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{} }) }

func (m *Model) Init() tea.Cmd { return poll() }

// extras loads the logs shown on screen, inside the same transaction.
func (m *Model) extras(tx *sql.Tx, s *world.State, p *world.Player) error {
	var err error
	if m.news, err = store.Events(tx, 0, 4); err != nil {
		return err
	}
	m.fight = nil
	if p.Battle != 0 {
		m.fight, err = store.BattleLog(tx, s.Season.ID, p.Battle, 6)
	} else if m.lastFight != 0 && !p.KO {
		// Someone else ended our fight: say how.
		if last, err := store.BattleLog(tx, s.Season.ID, m.lastFight, 1); err == nil && len(last) > 0 {
			m.msg = last[0].Text
		}
	}
	return err
}

func (m *Model) apply(res app.Result) {
	m.s, m.me = res.State, res.Player
	if m.me.Battle != 0 && m.lastFight != m.me.Battle {
		m.fightFrom, m.menu = time.Now(), 0
	}
	m.lastFight = m.me.Battle
}

func (m *Model) act(fn app.Action) {
	m.msg, m.msgBad = "", false
	res, err := app.Act(m.db, m.user, fn, m.extras)
	if err != nil {
		m.err = err
		return
	}
	m.apply(res)
	m.msg = res.Msg
	if res.Oops != nil {
		m.msg, m.msgBad = res.Oops.Error(), true
	}
}

func (m *Model) refresh() {
	res, stale, err := app.Peek(m.db, m.user, m.extras)
	switch {
	case err != nil:
		m.err = err
	case stale:
		m.act(nil)
	default:
		m.apply(res)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pollMsg:
		m.refresh()
		if m.err != nil {
			return m, tea.Quit
		}
		return m, poll()
	case tea.KeyMsg:
		m.key(msg.String())
		if m.err != nil || m.quit {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) key(k string) {
	switch {
	case k == "ctrl+c":
		m.quit = true
		return
	case m.help:
		m.help = false
		return
	case k == "?":
		m.help = true
		return
	case m.pkgMenu:
		m.pkgKey(k)
		return
	case k == "q" || k == "esc":
		m.quit = true
		return
	}

	if m.me.Battle != 0 {
		m.battleKey(k)
		return
	}
	dirs := map[string]string{
		"up": "up", "k": "up", "w": "up",
		"down": "down", "j": "down", "s": "down",
		"left": "left", "h": "left", "a": "left",
		"right": "right", "l": "right", "d": "right",
	}
	if dir, ok := dirs[k]; ok {
		m.act(func(s *world.State, p *world.Player) (string, error) { return game.Move(s, p, dir) })
		return
	}
	if k == "p" || k == "u" {
		m.openPackages()
	}
}

func (m *Model) battleKey(k string) {
	if time.Since(m.fightFrom) < battleWait {
		return
	}
	choice := -1
	switch k {
	case "up", "k":
		m.menu = (m.menu + len(battleOptions) - 1) % len(battleOptions)
	case "down", "j":
		m.menu = (m.menu + 1) % len(battleOptions)
	case "enter", " ":
		choice = m.menu
	case "1", "2", "3", "4":
		choice = int(k[0] - '1')
	}
	switch choice {
	case 0:
		m.act(game.Attack)
	case 1:
		m.act(game.Defend)
	case 2:
		m.openPackages()
	case 3:
		m.act(game.Flee)
	}
}

// usable lists the package kinds you hold that work right now, with counts.
func (m *Model) usable() []string {
	var out []string
	for _, kind := range world.PackageKinds {
		if m.s.Holding(m.user, kind) == nil {
			continue
		}
		if kind == world.Rimraf && m.me.Battle == 0 {
			continue
		}
		out = append(out, kind)
	}
	return out
}

func (m *Model) openPackages() {
	if len(m.usable()) == 0 {
		m.msg, m.msgBad = "You have no packages you can use right now.", true
		return
	}
	m.pkgMenu, m.pkgCursor = true, 0
}

func (m *Model) pkgKey(k string) {
	kinds := m.usable()
	switch k {
	case "esc", "q", "p", "u":
		m.pkgMenu = false
	case "up", "k":
		m.pkgCursor = (m.pkgCursor + len(kinds) - 1) % len(kinds)
	case "down", "j":
		m.pkgCursor = (m.pkgCursor + 1) % len(kinds)
	case "enter", " ":
		kind := kinds[min(m.pkgCursor, len(kinds)-1)]
		m.pkgMenu = false
		if m.me.Battle != 0 {
			m.act(func(s *world.State, p *world.Player) (string, error) { return game.UseInBattle(s, p, kind) })
		} else {
			m.act(func(s *world.State, p *world.Player) (string, error) { return game.UseOutside(s, p, kind) })
		}
	}
	if len(m.usable()) == 0 {
		m.pkgMenu = false
	}
}

func (m *Model) View() string {
	if m.s == nil || m.me == nil {
		return "Loading the Vault...\n"
	}
	if m.help {
		return render.Help + "\n" + render.Dim.Render("press any key to go back")
	}
	rm := m.s.Room(m.me.Room)
	head := fmt.Sprintf("%s  %s", render.Title.Render("THE VAULT"),
		render.Dim.Render(fmt.Sprintf("season %d · day %d · floor %d/%d · room %d", m.s.Season.ID, m.s.Day(), rm.Floor, world.Floors, rm.Num)))

	var left []string
	if m.me.Battle != 0 {
		left = m.battleView()
	} else {
		left = render.RoomLines(m.s, m.me, rm, m.me.X, m.me.Y, world.RoomW, world.RoomH)
		if m.pkgMenu {
			left = append(left, "", m.pkgView())
		}
	}
	left = append(left, "")
	if m.msg != "" {
		st := lipgloss.NewStyle().Bold(true)
		if m.msgBad {
			st = render.HPStyle
		}
		left = append(left, st.Width(leftWidth).Render(m.msg))
	}
	switch {
	case m.s.Won():
		left = append(left, render.Title.Render(fmt.Sprintf("Season won by %s! A new season starts at 3 AM ET (in %s).", m.s.Season.Winner, clock.Until(m.s.Now))))
	case m.me.KO:
		left = append(left, render.HPStyle.Render(fmt.Sprintf("You're knocked out. You'll come to at 3 AM ET (in %s).", clock.Until(m.s.Now))))
	}
	if m.me.Battle == 0 {
		for i := len(m.news) - 1; i >= 0; i-- {
			left = append(left, render.Dim.Width(leftWidth).Render("· "+m.news[i].Text))
		}
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(strings.Join(left, "\n")),
		"  ",
		lipgloss.NewStyle().Width(sideWidth).Render(strings.Join(m.sidebar(), "\n")))
	return head + "\n\n" + body + "\n\n" + render.Dim.Render(m.hints()) + "\n"
}

func (m *Model) hints() string {
	switch {
	case m.pkgMenu:
		return "↑↓ choose · enter use · esc back"
	case m.me.Battle != 0:
		return "↑↓ choose · enter confirm · 1-4 shortcuts · ? help · q quit"
	}
	return "arrows/hjkl/wasd move · p packages · ? help · q quit"
}

func (m *Model) battleView() []string {
	e := m.s.Enemy(m.me.Battle)
	if e == nil {
		return []string{"The fight is over."}
	}
	var lines []string
	name := render.EnemyGlyph(e) + " " + render.EnemyStyle.Render(fmt.Sprintf("Lv %d %s", e.Level, e.Name))
	lines = append(lines, name)
	lines = append(lines, render.HPStyle.Render("♥ ")+render.Bar(e.HP, e.MaxHP, 24, render.HPStyle)+fmt.Sprintf(" %d/%d", e.HP, e.MaxHP))
	lines = append(lines, "")
	var tags []string
	for _, p := range m.s.Fighters(e.ID) {
		tags = append(tags, render.PlayerTag(p, p == m.me)+" "+p.User)
	}
	team := strings.Join(tags, "  ")
	if n := len(tags) - 1; n > 0 {
		team += render.Dim.Render(fmt.Sprintf("  (+%d%% damage)", n*int(world.AllyBonus*100)))
	}
	lines = append(lines, "Fighting: "+team)
	if m.me.Braced {
		lines = append(lines, render.Dim.Render("You're braced: next attack +50%."))
	}
	lines = append(lines, "")
	if m.pkgMenu {
		lines = append(lines, m.pkgView())
	} else {
		for i, o := range battleOptions {
			cursor := "  "
			label := fmt.Sprintf("%-8s", o.label)
			if i == m.menu {
				cursor = render.Title.Render("▸ ")
				label = render.Bold.Render(label)
			}
			lines = append(lines, fmt.Sprintf("%s%d %s %s", cursor, i+1, label, render.Dim.Render(o.cost)))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("197")).
		Padding(0, 1).Width(leftWidth - 2).Render(strings.Join(lines, "\n"))
	out := strings.Split(box, "\n")
	for i := len(m.fight) - 1; i >= 0; i-- {
		out = append(out, render.Dim.Render("· "+m.fight[i].Text))
	}
	return out
}

func (m *Model) pkgView() string {
	var lines []string
	lines = append(lines, render.Bold.Render("Use which package?"))
	for i, kind := range m.usable() {
		count := 0
		for _, it := range m.s.Held(m.user) {
			if it.Kind == kind {
				count++
			}
		}
		cursor := "  "
		if i == m.pkgCursor {
			cursor = render.Title.Render("▸ ")
		}
		lines = append(lines, fmt.Sprintf("%s%s ×%d %s", cursor, render.PackageStyle.Render(kind), count, render.Dim.Render(pkgHelp[kind])))
	}
	return strings.Join(lines, "\n")
}

var pkgHelp = map[string]string{
	world.Patch:  "heal half your HP",
	world.Rimraf: "blast the enemy",
	world.Cache:  "+25 energy",
}

func (m *Model) sidebar() []string {
	me := m.me
	rm := m.s.Room(me.Room)
	lines := []string{
		render.PlayerTag(me, true) + " " + render.Bold.Render(me.User) + render.Dim.Render(fmt.Sprintf("  Lv %d", me.Level)),
		render.HPStyle.Render("♥ ") + render.Bar(me.HP, me.MaxHP(), 14, render.HPStyle) + fmt.Sprintf(" %d/%d", me.HP, me.MaxHP()),
		render.XPStyle.Render("✦ ") + render.Bar(me.XP, world.XPToNext(me.Level), 14, render.XPStyle) + fmt.Sprintf(" %d/%d", me.XP, world.XPToNext(me.Level)),
		render.EnergyStyle.Render("⚡") + " " + render.Bar(me.Energy, world.MaxEnergy, 14, render.EnergyStyle) + fmt.Sprintf(" %d", me.Energy),
		"",
		render.Dim.Render("packages"),
	}
	counts := map[string]int{}
	for _, it := range m.s.Held(m.user) {
		counts[it.Kind]++
	}
	if len(counts) == 0 {
		lines = append(lines, render.Dim.Render("  none yet"))
	}
	for _, kind := range world.PackageKinds {
		if n := counts[kind]; n > 0 {
			lines = append(lines, fmt.Sprintf("  %s ×%d", render.PackageStyle.Render(kind), n))
		}
	}
	lines = append(lines, "", render.Dim.Render(fmt.Sprintf("floor %d", rm.Floor)))
	for _, l := range render.Minimap(m.s, me, rm.Floor) {
		lines = append(lines, "  "+l)
	}
	lines = append(lines, "", render.Dim.Render("explorers"))
	for _, u := range m.s.Users() {
		p := m.s.Players[u]
		where := fmt.Sprintf("floor %d", m.s.Room(p.Room).Floor)
		switch {
		case p.KO:
			where = "knocked out"
		case p.Battle != 0:
			where = "fighting"
		case p.Room == me.Room:
			where = "here"
		}
		online := " "
		if m.s.Now.Sub(p.LastSeen) < 3*time.Minute {
			online = render.EnergyStyle.Render("●")
		}
		lines = append(lines, fmt.Sprintf("%s%s %-9s Lv %-2d %s", online, render.PlayerTag(p, p == me), trim(u, 9), p.Level, render.Dim.Render(where)))
	}
	return lines
}

func trim(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
