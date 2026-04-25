GO ?= go
BINARY ?= git-branch-prune
BIN_DIR ?= bin

.PHONY: help build test install fmt clean

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make build    Build the CLI into $(BIN_DIR)/$(BINARY)' \
		'  make test     Run the Go test suite' \
		'  make install  Install the CLI with go install .' \
		'  make fmt      Format Go sources with gofmt' \
		'  make clean    Remove built artifacts'

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(BINARY) .

test:
	$(GO) test ./...

install:
	$(GO) install .

fmt:
	$(GO) fmt ./...

clean:
	rm -rf $(BIN_DIR)
