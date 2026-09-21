GO ?= go
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/wal-example
COVERAGE_FILE ?= coverage.out

.PHONY: test bench run build

test:
	$(GO) test -coverprofile=$(COVERAGE_FILE) ./...
	$(GO) tool cover -func=$(COVERAGE_FILE)

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./...

run:
	$(GO) run ./examples/basic $(ARGS)

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) ./examples/basic
