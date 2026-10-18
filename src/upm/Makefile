PREFIX ?= /usr
DESTDIR ?=
BINDIR ?= $(PREFIX)/bin

GO ?= go
GOFLAGS ?= -ldflags="-s -w"
BIN := bin/upm

.PHONY: all build install clean test fmt vet

all: build

build:
	mkdir -p $(dir $(BIN))
	$(GO) build $(GOFLAGS) -o $(BIN) ./cmd/upm

install: build
	install -Dm755 $(BIN) $(DESTDIR)$(BINDIR)/upm

clean:
	rm -rf bin/

test:
	$(GO) test -v ./...

fmt:
	gofmt -s -w .

vet:
	$(GO) vet ./...
