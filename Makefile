GO ?= go
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/wal-example
INSPECT_BINARY ?= $(BIN_DIR)/wal-inspect

.PHONY: test bench run inspect build build-inspect

test:
	$(GO) test ./...

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./...

run:
	$(GO) run ./examples/basic $(ARGS)

inspect:
	$(GO) run ./cmd/wal-inspect $(ARGS)

build: build-inspect
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) ./examples/basic

build-inspect:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(INSPECT_BINARY) ./cmd/wal-inspect
