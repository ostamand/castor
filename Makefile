BINARY_NAME := castor
CMD_PATH := ./cmd/castor
INSTALL_PATH := /usr/local/bin

SKILLS_SRC := $(CURDIR)/skills
SKILLS_DEST := $(HOME)/.gemini/config/skills

.PHONY: all build test clean install install-skills uninstall-skills tidy fmt lint

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
	install -m 755 bin/$(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
