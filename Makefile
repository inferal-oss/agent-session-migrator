.DEFAULT_GOAL := all

GO ?= go
VERSION ?= dev
BUILD_DIR ?= bin
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
LICENSEDIR ?= $(PREFIX)/share/licenses/agent-session-migrator
DESTDIR ?=

BINARY := agent-session-migrator

.PHONY: all build install

all: build

build:
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o "$(BUILD_DIR)/$(BINARY)" ./cmd/agent-session-migrator

install: build
	install -d "$(DESTDIR)$(BINDIR)" "$(DESTDIR)$(LICENSEDIR)"
	install -m 0755 "$(BUILD_DIR)/$(BINARY)" "$(DESTDIR)$(BINDIR)/$(BINARY)"
	install -m 0644 LICENSE LICENSE-APACHE LICENSE-MIT NOTICE "$(DESTDIR)$(LICENSEDIR)/"
