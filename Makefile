.PHONY: build test version ladybug-lib test-ladybug build-linux-amd64

build: test version
	go build ./cmd/ladygraph

test:
	go test ./...

version:
	go run ./cmd/ladygraph version

# ladybug-lib downloads the pinned native library and verifies its digest.
ladybug-lib:
	@scripts/fetch-ladybug.sh

# test-ladybug runs the suites that need a real LadybugDB. The pinned core and
# the pinned Go binding must share a version: a mismatched pair segfaults on
# the first C call instead of failing a test.
test-ladybug:
	@LIB="$$(scripts/fetch-ladybug.sh)"; \
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$$LIB" \
	CGO_LDFLAGS="-L$$LIB -llbug -Wl,-rpath,$$LIB" \
	go test -tags ladybug ./...

# build-linux-amd64 creates the generated Linux amd64 distribution bundle.
build-linux-amd64:
	@scripts/build-linux-amd64.sh
