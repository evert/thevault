package game

import (
	"fmt"
	"math"

	"thevault/internal/world"
)

// engage starts a fight, or joins one others are already in. It's free.
func engage(s *world.State, p *world.Player, e *world.Enemy) string {
	p.Battle, p.Braced = e.ID, false
	p.Known[e.Name] = true // from now on you can read this type's level
	allies := len(s.Fighters(e.ID)) - 1
	if allies > 0 {
		s.BattleLog(e.ID, p.User, fmt.Sprintf("%s joined the fight!", p.User))
		return fmt.Sprintf("You join the fight against Lv %d %s.", e.Level, e.Name)
	}
	s.BattleLog(e.ID, p.User, fmt.Sprintf("%s engaged Lv %d %s.", p.User, e.Level, e.Name))
	if e.Boss {
		s.Emit(p.User, "boss", fmt.Sprintf("%s is fighting %s! Everyone, get down there.", p.User, e.Name))
	}
	return fmt.Sprintf("A Lv %d %s!", e.Level, e.Name)
}

// foe returns the enemy the player is fighting, ending the fight if it's gone.
func foe(s *world.State, p *world.Player) (*world.Enemy, error) {
	if err := canAct(s, p); err != nil {
		return nil, err
	}
	e := s.Enemy(p.Battle)
	if e == nil {
		p.Battle, p.Braced = 0, false
		return nil, oops("That fight is over.")
	}
	p.LastSeen = s.Now
	return e, nil
}

// roll varies damage by ±20%.
func roll(s *world.State, base float64) int {
	return max(1, int(math.Round(base*(0.8+0.4*s.Rng.Float64()))))
}

func Attack(s *world.State, p *world.Player) (string, error) {
	e, err := foe(s, p)
	if err != nil {
		return "", err
	}
	if err := spend(s, p, world.AttackCost); err != nil {
		return "", err
	}
	allies := len(s.Fighters(e.ID)) - 1
	base := float64(p.Atk()) * (1 + world.AllyBonus*float64(allies))
	if p.Braced {
		base *= 1.5
		p.Braced = false
	}
	dmg := roll(s, base)
	crit := s.Rng.IntN(10) == 0
	if crit {
		dmg *= 2
	}
	note := ""
	if crit {
		note = " Critical!"
	} else if allies > 0 {
		note = fmt.Sprintf(" (+%d%% teamwork)", int(world.AllyBonus*100)*allies)
	}
	msg := fmt.Sprintf("You hit for %d.%s", dmg, note)
	s.BattleLog(e.ID, p.User, fmt.Sprintf("%s hit for %d.%s", p.User, dmg, note))
	return msg + hurtEnemy(s, p, e, dmg), nil
}

func Defend(s *world.State, p *world.Player) (string, error) {
	e, err := foe(s, p)
	if err != nil {
		return "", err
	}
	if err := spend(s, p, world.DefendCost); err != nil {
		return "", err
	}
	p.Braced = true
	p.HP = min(p.MaxHP(), p.HP+max(1, p.MaxHP()/10))
	s.BattleLog(e.ID, p.User, fmt.Sprintf("%s braced.", p.User))
	return "You brace and catch your breath." + counter(s, p, e, true), nil
}

// UseInBattle uses a package mid-fight. The enemy still gets its swing.
func UseInBattle(s *world.State, p *world.Player, kind string) (string, error) {
	e, err := foe(s, p)
	if err != nil {
		return "", err
	}
	if world.IsToken(kind) {
		return "", oops("A token won't help you here.")
	}
	it := s.Holding(p.User, kind)
	if it == nil {
		return "", oops("You don't have a %s package.", kind)
	}
	if err := spend(s, p, world.PackageCost); err != nil {
		return "", err
	}
	s.RemoveItem(it)
	switch kind {
	case world.Rimraf:
		dmg := 10 + 5*s.Room(p.Room).Floor
		s.BattleLog(e.ID, p.User, fmt.Sprintf("%s ran rimraf for %d!", p.User, dmg))
		return fmt.Sprintf("rm -rf! %d damage.", dmg) + hurtEnemy(s, p, e, dmg), nil
	case world.Patch:
		heal(p)
	case world.Cache:
		p.Energy = min(world.MaxEnergy, p.Energy+25)
	}
	s.BattleLog(e.ID, p.User, fmt.Sprintf("%s used %s.", p.User, kind))
	return useText(kind) + counter(s, p, e, false), nil
}

// Flee leaves the fight. It's free and always works; the enemy keeps its wounds.
func Flee(s *world.State, p *world.Player) (string, error) {
	e := s.Enemy(p.Battle)
	p.Battle, p.Braced = 0, false
	if e != nil {
		s.BattleLog(e.ID, p.User, fmt.Sprintf("%s fled.", p.User))
	}
	return "You back away.", nil
}

// hurtEnemy applies damage, then either kills the enemy or lets it hit back.
func hurtEnemy(s *world.State, p *world.Player, e *world.Enemy, dmg int) string {
	e.HP -= dmg
	e.Hitters[p.User] = true
	if e.HP > 0 {
		return counter(s, p, e, false)
	}
	return " " + defeat(s, p, e)
}

// counter is the enemy hitting back at whoever just acted. Bracing halves it.
func counter(s *world.State, p *world.Player, e *world.Enemy, braced bool) string {
	if s.Rng.IntN(10) == 0 {
		s.BattleLog(e.ID, "", fmt.Sprintf("%s missed %s.", e.Name, p.User))
		return fmt.Sprintf(" %s misses!", e.Name)
	}
	dmg := roll(s, float64(e.Atk))
	if braced {
		dmg = max(1, dmg/2)
	}
	p.HP -= dmg
	if p.HP <= 0 {
		p.HP, p.KO, p.Battle, p.Braced = 0, true, 0, false
		s.BattleLog(e.ID, "", fmt.Sprintf("%s knocked out %s!", e.Name, p.User))
		s.Emit(p.User, "ko", fmt.Sprintf("%s was knocked out by Lv %d %s in room %d.", p.User, e.Level, e.Name, p.Room))
		dropped := ""
		if dropKeys(s, p) > 0 {
			dropped = " Your token clatters to the floor."
		}
		return fmt.Sprintf(" %s hits you for %d. Everything goes dark...%s", e.Name, dmg, dropped)
	}
	s.BattleLog(e.ID, "", fmt.Sprintf("%s hit %s for %d.", e.Name, p.User, dmg))
	return fmt.Sprintf(" %s hits you for %d.", e.Name, dmg)
}

// defeat removes the enemy and shares the XP with everyone who hit it.
func defeat(s *world.State, killer *world.Player, e *world.Enemy) string {
	xp := 5 * e.Level
	if e.Boss {
		xp = 150
	}
	var helpers []string
	for _, u := range s.Users() {
		p := s.Players[u]
		if p.Battle == e.ID {
			p.Battle, p.Braced = 0, false
		}
		if e.Hitters[u] {
			if p != killer {
				helpers = append(helpers, u)
			}
			gainXP(s, p, xp)
		}
	}
	s.RemoveEnemy(e)
	s.BattleLog(e.ID, killer.User, fmt.Sprintf("%s defeated %s!", killer.User, e.Name))

	if e.Boss {
		s.Season.WonTick, s.Season.Winner = s.Tick, killer.User
		s.Emit(killer.User, "won", fmt.Sprintf("%s landed the final blow on %s! Season %d won on day %d.",
			killer.User, e.Name, s.Season.ID, s.Day()))
		return fmt.Sprintf("%s collapses into a heap of tarballs. You won the season!", e.Name)
	}
	if e.Level >= s.Room(killer.Room).Floor+1 || len(helpers) > 0 {
		with := ""
		if len(helpers) > 0 {
			with = fmt.Sprintf(" with %s", joinNames(helpers))
		}
		s.Emit(killer.User, "kill", fmt.Sprintf("%s took down a Lv %d %s%s.", killer.User, e.Level, e.Name, with))
	}

	msg := fmt.Sprintf("The %s is defeated! +%d XP.", e.Name, xp)
	if s.Rng.IntN(5) < 2 {
		it := &world.Item{ID: s.NextItemID(), Kind: world.RandomPackage(s.Rng), Holder: killer.User}
		s.Items = append(s.Items, it)
		msg += fmt.Sprintf(" It dropped %s.", PackageName(s, it))
	}
	if token := world.Token(s.Room(e.Room).Floor); e.Key && token != "" {
		s.Items = append(s.Items, &world.Item{ID: s.NextItemID(), Kind: token, Holder: killer.User})
		s.Emit(killer.User, "key", fmt.Sprintf("%s took the %s token off a Lv %d %s.", killer.User, token, e.Level, e.Name))
		msg += fmt.Sprintf(" It was carrying the %s token! You take it.", token)
	}
	return msg
}

func gainXP(s *world.State, p *world.Player, xp int) {
	p.XP += xp
	for p.XP >= world.XPToNext(p.Level) {
		p.XP -= world.XPToNext(p.Level)
		p.Level++
		p.HP = p.MaxHP()
		s.Emit(p.User, "level", fmt.Sprintf("%s reached level %d.", p.User, p.Level))
	}
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	out := names[0]
	for _, n := range names[1 : len(names)-1] {
		out += ", " + n
	}
	return out + " and " + names[len(names)-1]
}
