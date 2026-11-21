// Package app runs one game action against the database: load the world,
// run any missed daily ticks, apply the action, save. The CLI commands and
// the TUI both go through here.
package app

import (
	"database/sql"

	"thevault/internal/clock"
	"thevault/internal/game"
	"thevault/internal/store"
	"thevault/internal/world"
)

// Action changes the world on behalf of a player. A game.Oops error is a
// player mistake: it's reported, but everything else still saves.
type Action func(s *world.State, p *world.Player) (string, error)

// View reads extra things (logs) inside the same transaction, after saving.
type View func(tx *sql.Tx, s *world.State, p *world.Player) error

type Result struct {
	State  *world.State
	Player *world.Player
	Msg    string
	Oops   error
}

// Act runs an action (nil just catches up and joins) in a write transaction.
func Act(db *store.DB, user string, act Action, view View) (Result, error) {
	now, err := clock.Now()
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = db.Tx(func(tx *sql.Tx) error {
		s, err := store.Load(tx, now)
		if err != nil {
			return err
		}
		if s.Season.ID == 0 {
			game.Start(s, game.NewSeed())
		}
		game.CatchUp(s, game.NewSeed)
		game.Rebalance(s)
		game.DropIdleKeys(s)
		game.Respawn(s)
		p := game.Join(s, user)
		res = Result{State: s, Player: p}
		if act != nil {
			res.Msg, res.Oops = act(s, p)
			if res.Oops != nil && !game.IsOops(res.Oops) {
				return res.Oops
			}
		}
		game.Visit(s)
		if err := store.Save(tx, s); err != nil {
			return err
		}
		if view != nil {
			return view(tx, s, p)
		}
		return nil
	})
	return res, err
}

// Peek reads the world without changing it. It reports stale=true when a
// tick is due or the player hasn't joined, meaning the caller should Act.
func Peek(db *store.DB, user string, view View) (res Result, stale bool, err error) {
	now, err := clock.Now()
	if err != nil {
		return Result{}, false, err
	}
	err = db.Read(func(tx *sql.Tx) error {
		s, err := store.Load(tx, now)
		if err != nil {
			return err
		}
		p := s.Players[user]
		if s.Season.ID == 0 || p == nil || clock.LatestBoundary(now).After(s.LastTick) || s.Balance < game.BalanceVersion {
			stale = true
			return nil
		}
		res = Result{State: s, Player: p}
		if view != nil {
			return view(tx, s, p)
		}
		return nil
	})
	return res, stale, err
}
