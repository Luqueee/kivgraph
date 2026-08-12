.PHONY: build test version ladybug-lib test-ladybug build-linux-amd64 build-darwin-arm64 site-check site-build

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
#
# The pinned library is not on the linker's default search path, and the
# binding points at a directory of its own module that the pinned build does
# not populate: `go test -tags ladybug` on its own fails to link with
# "library 'lbug' not found". These flags are the whole difference, which is
# why this target exists and why nobody should run the tag by hand.
#
# PKGS narrows the run while working on one package. The default is the whole
# suite, because that is what a release has to pass.
PKGS ?= ./...
test-ladybug:
	@LIB="$$(scripts/fetch-ladybug.sh)"; \
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$$LIB" \
	CGO_LDFLAGS="-L$$LIB -llbug -Wl,-rpath,$$LIB" \
	go test -tags ladybug $(PKGS)

# build-linux-amd64 and build-darwin-arm64 create the generated distribution
# bundle for each supported target. cgo links the pinned LadybugDB library, so
# a bundle is always built by a host of its own platform.
build-linux-amd64:
	@scripts/build-bundle.sh --target linux/amd64

build-darwin-arm64:
	@scripts/build-bundle.sh --target darwin/arm64

# site-check and site-build drive the landing and documentation site. It is not
# part of any release bundle: scripts/build-bundle.sh names its payload
# explicitly and never reads this directory.
site-check:
	@pnpm --dir site install --frozen-lockfile && pnpm --dir site check

site-build:
	@pnpm --dir site install --frozen-lockfile && pnpm --dir site build
