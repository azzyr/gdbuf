VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "none")
LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT)

.PHONY: build build-all
build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/gdbuf .

build-all:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/gdbuf-linux-amd64 .
	# GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/gdbuf-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/gdbuf-darwin-arm64 .
	# GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/gdbuf-windows-amd64.exe .
