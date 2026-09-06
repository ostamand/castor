BINARY_NAME := castor
CMD_PATH := ./cmd/castor
INSTALL_PATH := /usr/local/bin

.PHONY: all build test clean install tidy fmt lint

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

install: build
	install -m 755 bin/$(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
