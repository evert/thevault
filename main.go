// The Vault: a slow, shared, co-op dungeon for a multi-user dev server.
package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"

	"thevault/internal/clock"
	"thevault/internal/game"
	"thevault/internal/render"
	"thevault/internal/store"
	"thevault/internal/world"
)

// defaultDB is where the shared world lives. `make build` overrides it with a
// local test file; `make release` and plain `go build` keep the server path.
var defaultDB = "/var/lib/thevault/thevault.db"

func main() {
	setUmask()
	args := os.Args[1:]
	dbPath := os.Getenv("THEVAULT_DB")
	if dbPath == "" {
		dbPath = defaultDB
	}
	if len(args) >= 2 && args[0] == "--db" {
		dbPath, args = args[1], args[2:]
	}
	st := render.Style{Color: term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""}
	err := run(dbPath, currentUser(), args, os.Stdout, st)
	switch {
	case err == nil:
	case game.IsOops(err):
		fmt.Fprintln(os.Stdout, err)
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "thevault:", err)
		os.Exit(2)
	}
}

// explainOpenErr turns SQLite's "unable to open database file" into advice.
func explainOpenErr(path string, err error) error {
	dir := filepath.Dir(path)
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		return fmt.Errorf("the save folder %s doesn't exist.\n"+
			"  On the server: run `sudo make install` (see deploy/README.md).\n"+
			"  To play locally: THEVAULT_DB=./thevault.db ./thevault", dir)
	}
	if !writable(dir) {
		return fmt.Errorf("you can't write to %s.\n"+
			"  Ask an admin to run `usermod -aG thevault $USER`, then log out and back in.", dir)
	}
	return err
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "nobody"
}

// run executes one command. The tick, the action and the saving all happen
// in one transaction; player mistakes (game.Oops) still commit, so a tick
// triggered by a bad command isn't lost.
func run(dbPath, me string, args []string, out io.Writer, st render.Style) error {
	cmd, rest := "status", []string(nil)
	if len(args) > 0 {
		cmd, rest = strings.ToLower(args[0]), args[1:]
	}
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Fprint(out, render.Help)
		fmt.Fprintf(out, "\nSave file: %s\n", dbPath)
		return nil
	}
	arg := func(i int) string {
		if i < len(rest) {
			return rest[i]
		}
		return ""
	}

	now, err := clock.Now()
	if err != nil {
		return err
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return explainOpenErr(dbPath, err)
	}
	defer db.Close()

	var text string
	var oops error
	err = db.Tx(func(tx *sql.Tx) error {
		s, err := store.Load(tx, now)
		if err != nil {
			return err
		}
		if s.Season.ID == 0 {
			game.Start(s, game.NewSeed())
		}
		game.CatchUp(s, game.NewSeed)
		p := game.Join(s, me)

		var msg string
		switch cmd {
		case "status", "look", "map", "who", "log", "motd":
			// Read-only; rendered below.
		case "go", "move":
			msg, oops = game.Go(s, p, arg(0))
		case "n", "s", "e", "w", "north", "south", "east", "west":
			msg, oops = game.Go(s, p, cmd)
		case "search":
			msg, oops = game.Search(s, p)
		case "use":
			msg, oops = game.Use(s, p, strings.ToLower(arg(0)), arg(1))
		case "open", "unpack":
			msg, oops = game.Open(s, p)
		case "descend", "down":
			msg, oops = game.Descend(s, p)
		case "ascend", "up":
			msg, oops = game.Ascend(s, p)
		case "rest":
			msg, oops = game.Rest(s, p)
		case "revive":
			msg, oops = game.Revive(s, p, arg(0))
		case "give":
			msg, oops = game.Give(s, p, arg(0), strings.ToLower(arg(1)))
		case "note":
			msg, oops = game.Note(s, p, strings.Join(rest, " "))
		default:
			oops = game.Oops(fmt.Sprintf("Unknown command %q. Try `thevault help`.", cmd))
		}
		if err := store.Save(tx, s); err != nil {
			return err
		}
		if oops != nil {
			return nil
		}

		var b strings.Builder
		if msg != "" {
			b.WriteString(msg + "\n\n")
		}
		switch cmd {
		case "map":
			depth := s.Room(p.Room).Depth
			if n, err := strconv.Atoi(arg(0)); err == nil && n >= 1 && n <= world.Depths {
				depth = n
			}
			b.WriteString(render.Map(st, s, p, depth))
		case "who":
			b.WriteString(render.Who(st, s, p))
		case "log":
			events, err := store.Events(tx, 0, 25)
			if err != nil {
				return err
			}
			b.WriteString(render.Log(st, s, events))
		case "motd":
			events, err := store.Events(tx, s.Tick-1, 100)
			if err != nil {
				return err
			}
			b.WriteString(render.Motd(st, s, p, events))
		case "look", "status":
			notes, err := store.Notes(tx, s.Season.ID, p.Room, 3)
			if err != nil {
				return err
			}
			if cmd == "look" {
				b.WriteString(render.Look(st, s, p, notes))
			} else {
				b.WriteString(render.Status(st, s, p, notes))
			}
		default:
			// After an action, show where you are now.
			notes, err := store.Notes(tx, s.Season.ID, p.Room, 3)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s  turns left: %d\n", render.Hearts(p), p.Turns)
			b.WriteString(render.Look(st, s, p, notes))
		}
		text = b.String()
		return nil
	})
	if err != nil {
		return err
	}
	if oops != nil {
		return oops
	}
	fmt.Fprint(out, text)
	return nil
}
