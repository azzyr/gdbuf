VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "none")
LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT)

.PHONY: build
build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/gdbuf .
