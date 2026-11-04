.PHONY: build test lint fmt

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	golangci-lint fmt
