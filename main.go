// The Vault: a shared co-op dungeon crawler for a multi-user dev server.
package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"thevault/internal/app"
	"thevault/internal/game"
	"thevault/internal/render"
	"thevault/internal/store"
	"thevault/internal/tui"
	"thevault/internal/world"
)

// defaultDB is where the shared world lives. `make build` overrides it with a
// local test file; `make release` and plain `go build` keep the server path.
var defaultDB = "/var/lib/thevault/thevault.db"

// devMode is set by `make build` to turn on dev-only cheats. It's a build
// flag rather than an env var so players on the server can't switch it on.
var devMode = ""

func main() {
	setUmask()
	game.DevEnergy = devMode != ""
	args := os.Args[1:]
	dbPath := os.Getenv("THEVAULT_DB")
	if dbPath == "" {
		dbPath = defaultDB
	}
	if len(args) >= 2 && args[0] == "--db" {
		dbPath, args = args[1], args[2:]
	}
	if err := run(dbPath, currentUser(), args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "thevault:", err)
		os.Exit(1)
	}
}

// explainOpenErr turns SQLite's "unable to open database file" into advice.
func explainOpenErr(path string, err error) error {
	dir := filepath.Dir(path)
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		return fmt.Errorf("the save folder %s doesn't exist.\n"+
			"  On the server: run `sudo make install` (see deploy/README.md).\n"+
			"  To play locally: `make build`, then ./thevault", dir)
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

func run(dbPath, me string, args []string, out io.Writer) error {
	cmd := "play"
	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
	}
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Fprint(out, render.Help)
		fmt.Fprintf(out, "\nSave file: %s\n", dbPath)
		return nil
	}
	if cmd != "play" && cmd != "motd" && cmd != "who" && cmd != "log" {
		return fmt.Errorf("unknown command %q (try `thevault help`)", cmd)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		return explainOpenErr(dbPath, err)
	}
	defer db.Close()

	if cmd == "play" {
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			cmd = "motd" // no terminal to draw in: show the summary instead
		} else {
			return tui.Run(db, me)
		}
	}

	var text string
	_, err = app.Act(db, me, nil, func(tx *sql.Tx, s *world.State, p *world.Player) error {
		switch cmd {
		case "motd":
			events, err := store.Events(tx, s.Tick-1, 100)
			if err != nil {
				return err
			}
			text = render.Motd(s, p, events)
		case "who":
			text = render.Who(s, p)
		case "log":
			events, err := store.Events(tx, 0, 25)
			if err != nil {
				return err
			}
			text = render.Log(events)
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprint(out, text)
	return nil
}
