# Deploying The Vault

The game is one static binary plus a SQLite file that every player writes to.
Players run `thevault` for the full-screen game; the login hook runs `thevault motd`.
There's no daemon and no cron job: the daily 3 AM ET (Toronto) tick runs inside
whichever command first notices the boundary has passed.

## One-time setup (as root on the dev server)

```sh
make install                         # binary, login hook, group, data dir
usermod -aG thevault alice           # repeat for each player
```

Building on another machine works too: `make release` there (not `make build`,
which makes a local test binary), copy `thevault` over, then run the `install`
lines from the Makefile by hand.

Players must log out and back in once for the new group to take effect.

## How sharing works

- `/var/lib/thevault` is owned by group `thevault` with the setgid bit (`2775`),
  so every file created there belongs to that group.
- The game sets its umask to `002` and makes the database group-writable when
  it's created; SQLite gives its `-wal` and `-shm` files the same mode.
- Nothing stops a player from editing the database directly. That's on purpose:
  we trust each other.

## Login news

`/etc/profile.d/thevault.sh` runs `thevault motd` for interactive logins. It's
capped at 2 seconds and stays silent on errors. If your distro doesn't source
`/etc/profile.d` for zsh, add the same line to `/etc/zsh/zprofile`.

## Testing locally

`make build` makes a test binary that saves to `./thevault-dev.db` instead of
the shared folder. `make clean-dev` wipes that world. `thevault help` shows
which save file a binary uses.

```sh
make build
USER=alice ./thevault                             # play as anyone
USER=bob ./thevault                               # ...in a second terminal, to test live co-op
THEVAULT_NOW=2026-10-30T09:00:00-04:00 ./thevault motd   # pretend it's this time
```

`THEVAULT_DB=/some/path.db` overrides the save file for any binary.

## Resetting

Delete `/var/lib/thevault/thevault.db*`. The next command starts season 1.
