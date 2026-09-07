BINARY_NAME := castor
CMD_PATH := ./cmd/castor
PREFIX ?= $(shell if [ -w /usr/local/bin ]; then echo /usr/local; else echo $(HOME)/.local; fi)
INSTALL_PATH ?= $(PREFIX)/bin

# Load .env file if present
ifneq (,$(wildcard .env))
    include .env
    export
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
CLI_PKG := github.com/ostamand/castor/internal/cli
AUTH_PKG := github.com/ostamand/castor/internal/auth

LDFLAGS := -s -w -X $(CLI_PKG).version=$(VERSION)
ifneq ($(CASTOR_GOOGLE_CLIENT_ID),)
    LDFLAGS += -X $(AUTH_PKG).defaultClientID=$(CASTOR_GOOGLE_CLIENT_ID)
endif
ifneq ($(CASTOR_GOOGLE_CLIENT_SECRET),)
    LDFLAGS += -X $(AUTH_PKG).defaultClientSecret=$(CASTOR_GOOGLE_CLIENT_SECRET)
endif
ifneq ($(CASTOR_DROPBOX_APP_KEY),)
    LDFLAGS += -X $(AUTH_PKG).defaultDropboxAppKey=$(CASTOR_DROPBOX_APP_KEY)
endif

SKILLS_SRC := $(CURDIR)/skills
SKILLS_DEST := $(HOME)/.gemini/config/skills

.PHONY: all build test clean dist install install-skills uninstall-skills uninstall uninstall-all tidy fmt lint

all: build

build:
	@mkdir -p bin
	@go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME) $(CMD_PATH)
	@echo "✔ Built bin/$(BINARY_NAME)"

test:
	go test -v -race ./...

dist: test
	@mkdir -p dist
	@echo "Compiling multi-platform release binaries..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/castor-linux-amd64 $(CMD_PATH)
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/castor-linux-arm64 $(CMD_PATH)
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/castor-darwin-arm64 $(CMD_PATH)
	@CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/castor-darwin-amd64 $(CMD_PATH)
	@cd dist && sha256sum castor-* > checksums.txt
	@echo "✔ Compiled release assets in dist/:"
	@ls -lh dist/

tidy:
	go mod tidy

fmt:
	go fmt ./...

clean:
	rm -rf bin/ dist/

install-skills:
	@mkdir -p $(SKILLS_DEST)
	@for skill in $(wildcard skills/*); do \
		name=$$(basename $$skill); \
		ln -sfn $(CURDIR)/skills/$$name $(SKILLS_DEST)/$$name; \
		echo "Symlinked skill: $$name -> $(SKILLS_DEST)/$$name"; \
	done

uninstall-skills:
	@for skill in $(wildcard skills/*); do \
		name=$$(basename $$skill); \
		rm -f $(SKILLS_DEST)/$$name; \
		echo "Removed skill symlink: $(SKILLS_DEST)/$$name"; \
	done

install: build install-skills
	@mkdir -p $(INSTALL_PATH)
	install -m 755 bin/$(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "✔ Castor installed to $(INSTALL_PATH)/$(BINARY_NAME)"

uninstall: uninstall-skills
	@systemctl --user disable --now castor.timer 2>/dev/null || true
	@rm -f $(HOME)/.config/systemd/user/castor.service $(HOME)/.config/systemd/user/castor.timer
	@systemctl --user daemon-reload 2>/dev/null || true
	@rm -f $(INSTALL_PATH)/$(BINARY_NAME)
	@rm -f $(HOME)/.local/bin/$(BINARY_NAME)
	@if [ -w /usr/local/bin ]; then rm -f /usr/local/bin/$(BINARY_NAME); fi
	@echo "✔ Castor binary, systemd timer, and skills uninstalled."

uninstall-all: uninstall
	@rm -rf $(HOME)/.config/castor
	@echo "✔ Removed Castor configuration and local state (~/.config/castor)."
