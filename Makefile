.PHONY: build test version ladybug-lib test-ladybug build-linux-amd64 build-darwin-arm64 landing-check landing-build

build: test version
	go build ./cmd/kivgraph

test:
	go test ./...

version:
	go run ./cmd/kivgraph version

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
# suite, because that is what a release has to pass. ARGS passes flags through
# to `go test`, which is the only way to reach a benchmark that needs this tag:
# the flags cannot be appended to the target, because make would read them as
# targets of its own.
PKGS ?= ./...
ARGS ?=
test-ladybug:
	@LIB="$$(scripts/fetch-ladybug.sh)"; \
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$$LIB" \
	CGO_LDFLAGS="-L$$LIB -llbug -Wl,-rpath,$$LIB" \
	go test -tags ladybug $(ARGS) $(PKGS)

# build-linux-amd64 and build-darwin-arm64 create the generated distribution
# bundle for each supported target. cgo links the pinned LadybugDB library, so
# a bundle is always built by a host of its own platform.
build-linux-amd64:
	@scripts/build-bundle.sh --target linux/amd64

build-darwin-arm64:
	@scripts/build-bundle.sh --target darwin/arm64

# landing-check and landing-build drive the landing and documentation site. It
# is not part of any release bundle: scripts/build-bundle.sh names its payload
# explicitly and never reads this directory.
landing-check:
	@pnpm --dir landing install --frozen-lockfile && pnpm --dir landing check

landing-build:
	@pnpm --dir landing install --frozen-lockfile && pnpm --dir landing build
