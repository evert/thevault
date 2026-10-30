package game

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"thevault/internal/clock"
	"thevault/internal/world"
)

// Oops is a player mistake: shown to them, but the transaction still commits.
type Oops string

func (o Oops) Error() string { return string(o) }

func oops(format string, a ...any) error { return Oops(fmt.Sprintf(format, a...)) }

// IsOops reports whether err is a player mistake rather than a failure.
func IsOops(err error) bool {
	var o Oops
	return errors.As(err, &o)
}

// Join adds a player at camp the first time they run a command.
func Join(s *world.State, user string) *world.Player {
	if p, ok := s.Players[user]; ok {
		return p
	}
	p := &world.Player{User: user, Room: s.Camp().Num, HP: world.MaxHP, Turns: world.TurnsPerTick, Joined: s.Now}
	s.Players[user] = p
	s.Emit(user, "join", fmt.Sprintf("%s joined the expedition.", user))
	return p
}

// spend checks the player can act and takes one turn.
func spend(s *world.State, p *world.Player) error {
	switch {
	case s.Won():
		return oops("This season is over. A new one starts at 3 AM ET (in %s).", clock.Until(s.Now))
	case p.Downed:
		return oops("You're downed. Someone in room %d can `revive %s`, or you'll be dragged out at 3 AM ET.", p.Room, p.User)
	case p.Turns <= 0:
		return oops("You're out of turns. You get %d more at 3 AM ET (in %s).", world.TurnsPerTick, clock.Until(s.Now))
	}
	p.Turns--
	return nil
}

// target resolves "n", "north" or a room number to an adjacent room and the door to it.
func target(s *world.State, p *world.Player, arg string) (*world.Room, *world.Door, error) {
	here := s.Room(p.Room)
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		return nil, nil, oops("Go where? Try a direction (n/s/e/w) or a room number.")
	}
	var there *world.Room
	var fail error
	if n, err := strconv.Atoi(arg); err == nil {
		there = s.Room(n)
		fail = oops("You can't get to room %d from here.", n)
	} else {
		dir := arg[:1]
		off, ok := world.Dirs[dir]
		if !ok || (len(arg) > 1 && world.DirNames[dir] != arg) {
			return nil, nil, oops("%q isn't a direction. Use n, s, e or w.", arg)
		}
		there = s.RoomAt(here.Depth, here.X+off[0], here.Y+off[1])
		fail = oops("There's no door to the %s.", world.DirNames[dir])
	}
	if there == nil {
		return nil, nil, fail
	}
	door := s.DoorBetween(here.Num, there.Num)
	if door == nil || !door.Revealed {
		return nil, nil, fail
	}
	return there, door, nil
}

func Go(s *world.State, p *world.Player, arg string) (string, error) {
	there, door, err := target(s, p, arg)
	if err != nil {
		return "", err
	}
	switch door.Kind {
	case world.Locked:
		return "", oops("That door is locked. `use key %s` if you have one.", world.DirTo(s.Room(p.Room), there))
	case world.Plate:
		held := false
		for _, o := range s.PlayersIn(door.Plate) {
			if o != p {
				held = true
			}
		}
		if !held {
			return "", oops("That door is sealed. It opens while someone else stands on the pressure plate in room %d.", door.Plate)
		}
	}
	if err := spend(s, p); err != nil {
		return "", err
	}
	p.Room = there.Num
	var b strings.Builder
	fmt.Fprintf(&b, "You go %s into room %d.", world.DirNames[world.DirTo(s.Room(door.Other(there.Num)), there)], there.Num)
	if world.Reveal(s, there) {
		b.WriteString(" Nobody has been here before.")
		switch there.Kind {
		case world.Stairs:
			if there.Depth == world.Depths {
				s.Emit(p.User, "stairs", fmt.Sprintf("%s found the Deep Gate on depth %d.", p.User, there.Depth))
			} else {
				s.Emit(p.User, "stairs", fmt.Sprintf("%s found the stairs down on depth %d (room %d).", p.User, there.Depth, there.Num))
			}
		case world.Tarball:
			s.Emit(p.User, "tarball", fmt.Sprintf("%s found a sealed tarball in room %d. It needs two keys or two people.", p.User, there.Num))
		}
	}
	if there.Murk {
		b.WriteString(" The Murk is thick here. Don't end the day in it.")
	}
	return b.String(), nil
}

func Search(s *world.State, p *world.Player) (string, error) {
	if err := spend(s, p); err != nil {
		return "", err
	}
	var found []string
	for _, it := range s.Items {
		if it.Hidden && it.Room == p.Room {
			it.Hidden, it.Room, it.Holder = false, 0, p.User
			found = append(found, PackageName(s, it))
		}
	}
	var doors []string
	for _, d := range s.DoorsOf(p.Room) {
		if d.Hidden {
			d.Hidden, d.Revealed = false, true
			doors = append(doors, world.DirNames[world.DirTo(s.Room(p.Room), s.Room(d.Other(p.Room)))])
		}
	}
	if len(found) == 0 && len(doors) == 0 {
		return "You search every corner. Nothing.", nil
	}
	var parts []string
	if len(found) > 0 {
		parts = append(parts, "You installed "+strings.Join(found, ", ")+".")
		s.Emit(p.User, "found", fmt.Sprintf("%s installed %s in room %d.", p.User, strings.Join(found, ", "), p.Room))
	}
	if len(doors) > 0 {
		parts = append(parts, "You found a hidden door to the "+strings.Join(doors, " and ")+".")
	}
	return strings.Join(parts, " "), nil
}

func Use(s *world.State, p *world.Player, what, arg string) (string, error) {
	here := s.Room(p.Room)
	switch what {
	case world.Key:
		key := s.Holding(p.User, world.Key)
		if key == nil {
			return "", oops("You don't have a key.")
		}
		if arg == "" && here.Kind == world.Tarball && !here.BoxOpen {
			if err := spend(s, p); err != nil {
				return "", err
			}
			key.Holder = ""
			here.BoxKeys++
			if here.BoxKeys >= 2 {
				return "You turn the second key. " + openBox(s, p, here), nil
			}
			return "You turn a key in the tarball's seal. One more key, or a second person, will open it.", nil
		}
		var door *world.Door
		if arg != "" {
			_, d, err := target(s, p, arg)
			if err != nil {
				return "", err
			}
			door = d
		} else {
			for _, d := range s.DoorsOf(here.Num) {
				if d.Revealed && d.Kind == world.Locked {
					if door != nil {
						return "", oops("More than one locked door here. Say which: `use key <dir>`.")
					}
					door = d
				}
			}
		}
		if door == nil || door.Kind != world.Locked {
			return "", oops("There's no locked door there.")
		}
		if err := spend(s, p); err != nil {
			return "", err
		}
		key.Holder = ""
		door.Kind = world.Open
		s.Emit(p.User, "unlock", fmt.Sprintf("%s unlocked the door between rooms %d and %d.", p.User, door.A, door.B))
		return fmt.Sprintf("The lock clunks open. The way between rooms %d and %d is clear for everyone.", door.A, door.B), nil

	case world.Torch:
		torch := s.Holding(p.User, world.Torch)
		if torch == nil {
			return "", oops("You don't have a torch.")
		}
		if err := spend(s, p); err != nil {
			return "", err
		}
		torch.Holder = ""
		was := here.Murk
		here.Murk = false
		here.SafeUntil = s.Tick + 2
		if was {
			s.Emit(p.User, "torch", fmt.Sprintf("%s burned the Murk out of room %d.", p.User, here.Num))
			return "The torch roars and the Murk recoils. This room stays clear for two nights.", nil
		}
		return "You plant the torch. The Murk won't come in here for two nights.", nil

	case world.Rope:
		return "", oops("Ropes work by themselves: one will save you from the Murk overnight.")
	case world.Relic:
		return "", oops("The Lost Package hums. Carry it through the Deep Gate on depth %d.", world.Depths)
	case "":
		return "", oops("Use what? Try `use key`, `use torch`.")
	}
	return "", oops("You don't have a %q.", what)
}

func Open(s *world.State, p *world.Player) (string, error) {
	here := s.Room(p.Room)
	if here.Kind != world.Tarball {
		return "", oops("There's nothing to open here.")
	}
	if here.BoxOpen {
		return "", oops("This tarball is already unpacked.")
	}
	if len(s.PlayersIn(here.Num)) < 2 {
		return "", oops("The seal needs two people pulling at once, or two keys (`use key`).")
	}
	if err := spend(s, p); err != nil {
		return "", err
	}
	return openBox(s, p, here), nil
}

func openBox(s *world.State, p *world.Player, box *world.Room) string {
	box.BoxOpen = true
	var got []string
	relic := false
	for _, it := range s.Items {
		if it.InBox && it.Room == box.Num {
			it.InBox, it.Room, it.Holder = false, 0, p.User
			got = append(got, PackageName(s, it))
			relic = relic || it.Kind == world.Relic
		}
	}
	if relic {
		s.Emit(p.User, "relic", fmt.Sprintf("%s unpacked the Lost Package! Now get it through the Deep Gate.", p.User))
		return "The tarball unpacks... THE LOST PACKAGE. It's yours. Take it through the Deep Gate."
	}
	s.Emit(p.User, "tarball_open", fmt.Sprintf("%s unpacked the tarball in room %d.", p.User, box.Num))
	return "The tarball unpacks. You installed " + strings.Join(got, ", ") + "."
}

func Descend(s *world.State, p *world.Player) (string, error) {
	here := s.Room(p.Room)
	if here.Kind != world.Stairs {
		return "", oops("There are no stairs down here.")
	}
	if here.Depth == world.Depths {
		if s.Holding(p.User, world.Relic) == nil {
			return "", oops("The Deep Gate won't budge. It wants the Lost Package.")
		}
		if err := spend(s, p); err != nil {
			return "", err
		}
		s.Season.WonTick = s.Tick
		s.Season.Winner = p.User
		s.Emit(p.User, "won", fmt.Sprintf("%s carried the Lost Package through the Deep Gate! Season %d won on day %d.", p.User, s.Season.ID, s.Day()))
		return "The Gate swallows the Lost Package and light floods the Vault. You did it. A new season starts at 3 AM ET.", nil
	}
	if err := spend(s, p); err != nil {
		return "", err
	}
	below := s.FindKind(here.Depth+1, world.Entrance)
	p.Room = below.Num
	if world.Reveal(s, below) {
		s.Emit(p.User, "depth", fmt.Sprintf("%s is the first to reach depth %d.", p.User, below.Depth))
		return fmt.Sprintf("You climb down into depth %d. You're the first one here.", below.Depth), nil
	}
	return fmt.Sprintf("You climb down into depth %d.", below.Depth), nil
}

func Ascend(s *world.State, p *world.Player) (string, error) {
	here := s.Room(p.Room)
	if here.Kind != world.Entrance || here.Depth == 1 {
		return "", oops("There are no stairs up here.")
	}
	if err := spend(s, p); err != nil {
		return "", err
	}
	p.Room = s.FindKind(here.Depth-1, world.Stairs).Num
	return fmt.Sprintf("You climb back up to depth %d.", here.Depth-1), nil
}

func Rest(s *world.State, p *world.Player) (string, error) {
	if p.HP >= world.MaxHP {
		return "", oops("You're already at full health.")
	}
	if err := spend(s, p); err != nil {
		return "", err
	}
	p.HP++
	return "You catch your breath. +1 health.", nil
}

func Revive(s *world.State, p *world.Player, who string) (string, error) {
	o, ok := s.Players[who]
	switch {
	case who == "":
		return "", oops("Revive who?")
	case !ok || o.Room != p.Room:
		return "", oops("%s isn't here.", who)
	case !o.Downed:
		return "", oops("%s is fine.", who)
	}
	if err := spend(s, p); err != nil {
		return "", err
	}
	o.Downed, o.HP = false, 1
	s.Emit(p.User, "revive", fmt.Sprintf("%s revived %s in room %d.", p.User, who, p.Room))
	return fmt.Sprintf("You haul %s to their feet.", who), nil
}

// Give hands a package to someone in the same room. It's free.
func Give(s *world.State, p *world.Player, who, what string) (string, error) {
	o, ok := s.Players[who]
	if !ok || o.Room != p.Room || o == p {
		return "", oops("%s isn't here.", who)
	}
	it := s.Holding(p.User, what)
	if it == nil {
		return "", oops("You don't have a %q.", what)
	}
	it.Holder = who
	s.Emit(p.User, "give", fmt.Sprintf("%s gave %s to %s.", p.User, PackageName(s, it), who))
	return fmt.Sprintf("You gave %s to %s.", PackageName(s, it), who), nil
}

func Note(s *world.State, p *world.Player, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", oops("Write something: `note \"don't go east\"`.")
	}
	if len(text) > world.NoteMaxLen {
		return "", oops("Keep it under %d characters.", world.NoteMaxLen)
	}
	s.Notes = append(s.Notes, world.Note{Room: p.Room, User: p.User, Text: text, At: s.Now})
	s.Emit(p.User, "note", fmt.Sprintf("%s left a note in room %d.", p.User, p.Room))
	return fmt.Sprintf("You scratch your note into the wall of room %d.", p.Room), nil
}

// PackageName gives an item a stable npm-style name like "torch@2.4.1".
func PackageName(s *world.State, it *world.Item) string {
	if it.Kind == world.Relic {
		return "the Lost Package"
	}
	h := s.Season.Seed*2654435761 + uint64(it.ID)*40503
	h ^= h >> 13
	return fmt.Sprintf("%s@%d.%d.%d", it.Kind, h%4, (h>>8)%13, (h>>16)%20)
}
