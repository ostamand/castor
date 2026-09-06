BINARY_NAME := castor
CMD_PATH := ./cmd/castor
PREFIX ?= $(shell if [ -w /usr/local/bin ]; then echo /usr/local; else echo $(HOME)/.local; fi)
INSTALL_PATH ?= $(PREFIX)/bin

SKILLS_SRC := $(CURDIR)/skills
SKILLS_DEST := $(HOME)/.gemini/config/skills

.PHONY: all build test clean install install-skills uninstall-skills uninstall uninstall-all tidy fmt lint

all: build

build:
	go build -ldflags="-s -w" -o bin/$(BINARY_NAME) $(CMD_PATH)

test:
	go test -v -race ./...

tidy:
	go mod tidy

fmt:
	go fmt ./...

clean:
	rm -rf bin/

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
