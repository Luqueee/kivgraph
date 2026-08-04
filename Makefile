.PHONY: build format lint test check version

GO_FILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

build:
	go build ./cmd/luque

format:
	gofmt -w $(GO_FILES)

lint:
	go vet ./...
	go tool staticcheck ./...

test:
	go test ./...

check:
	@test -z "$$(gofmt -l $(GO_FILES))"
	$(MAKE) lint
	$(MAKE) test

version: build
	./luque version
