PREFIX ?= ~/.local/bin

.PHONY: build install test clean

build:
	go build -o grove ./cmd/grove

install: build
	ln -sf $(CURDIR)/grove $(PREFIX)/grove

test:
	go test ./... -count=1

clean:
	rm -f grove
