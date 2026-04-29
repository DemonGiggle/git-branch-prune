GO ?= go
BINARY ?= git-branch-prune
ALIAS ?= git-brp
BIN_DIR ?= bin

.PHONY: help build test install fmt clean

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make build    Build the CLI into $(BIN_DIR)/$(BINARY) and $(BIN_DIR)/$(ALIAS)' \
		'  make test     Run the Go test suite' \
		'  make install  Install $(BINARY) and add the $(ALIAS) alias into the Go bin directory' \
		'  make fmt      Format Go sources with gofmt' \
		'  make clean    Remove built artifacts'

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(BINARY) .
	ln -sfn $(BINARY) $(BIN_DIR)/$(ALIAS)

test:
	$(GO) test ./...

install:
	$(GO) install .
	@install_dir="$$( $(GO) env GOBIN )"; \
	if [ -z "$$install_dir" ]; then install_dir="$$( $(GO) env GOPATH )/bin"; fi; \
	binary_path="$$install_dir/$(BINARY)"; \
	alias_path="$$install_dir/$(ALIAS)"; \
	mkdir -p "$$install_dir"; \
	ln -sfn $(BINARY) "$$alias_path"; \
	printf 'Installed binary: %s\n' "$$binary_path"; \
	printf 'Linked alias: %s -> %s\n' "$$alias_path" "$(BINARY)"

fmt:
	$(GO) fmt ./...

clean:
	rm -rf $(BIN_DIR)
