.PHONY: build test lint clean fmt install

VERSION := $(shell tr -d '[:space:]' < VERSION 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/rover ./cmd/rover

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/rover

test:
	go test ./...

fmt:
	gofmt -w .

lint:
	which golangci-lint 2>/dev/null || curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(shell go env GOPATH)/bin
	golangci-lint run ./...

clean:
	rm -rf bin/
