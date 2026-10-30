.PHONY: build release test install clean-dev

GOBUILD = CGO_ENABLED=0 go build -trimpath

# Local testing: saves to ./thevault-dev.db so you never touch the real world.
build:
	$(GOBUILD) -ldflags="-s -w -X main.defaultDB=thevault-dev.db" -o thevault .

# For the server: saves to /var/lib/thevault/thevault.db.
release:
	$(GOBUILD) -ldflags="-s -w" -o thevault .

test:
	go test ./...

# Wipe the local test world.
clean-dev:
	rm -f thevault-dev.db thevault-dev.db-wal thevault-dev.db-shm

# Run on the server as root (see deploy/README.md).
install: release
	install -m 0755 thevault /usr/local/bin/thevault
	install -m 0644 deploy/thevault.sh /etc/profile.d/thevault.sh
	getent group thevault >/dev/null || groupadd thevault
	install -d -g thevault -m 2775 /var/lib/thevault
