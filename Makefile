PREFIX ?= ~/.local/bin
MODULE := $(shell go list -m)
VERSION ?= $(shell { git describe --tags --match 'v*' --dirty --always 2>/dev/null || echo dev; } | sed 's/^v//')
LDFLAGS := -s -w -X $(MODULE)/internal/cmd.Version=$(VERSION)

.PHONY: build install test clean version release-check release release-patch release-minor release-major

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o grove ./cmd/grove

install: build
	ln -sf $(CURDIR)/grove $(PREFIX)/grove

test:
	go test ./... -count=1

version:
	@printf '%s\n' '$(VERSION)'

release-check:
	goreleaser check

release:
	@test -n "$(BUMP)" || (echo "Usage: make release BUMP=patch|minor|major [PUSH=1]" >&2; exit 1)
	./scripts/release.sh $(BUMP) $(if $(PUSH),--push,)

release-patch:
	./scripts/release.sh patch $(if $(PUSH),--push,)

release-minor:
	./scripts/release.sh minor $(if $(PUSH),--push,)

release-major:
	./scripts/release.sh major $(if $(PUSH),--push,)

clean:
	rm -f grove
