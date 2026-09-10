GO ?= go
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/wal-example

.PHONY: test bench run build

test:
	$(GO) test ./...

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./...

run:
	$(GO) run ./examples/basic $(ARGS)

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) ./examples/basic
