.PHONY: build test semantic-coverage version ladybug-lib test-ladybug build-linux-amd64 build-darwin-arm64 landing-check landing-build

build: test version
	go build ./cmd/kivgraph

test:
	go test ./...

semantic-coverage:
	scripts/verify-semantic-coverage.sh

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
# What the flags do NOT repeat matters as much as what they carry. The binding
# already declares `-llbug` and an rpath of its own, and `CGO_LDFLAGS` is
# applied to every cgo package in the build rather than once at the link, so
# naming either here made the linker see ten copies of the same rpath and two
# of the same library: 22 warnings for two packages, none of them a defect and
# all of them hiding one. So `-L` is all this carries -- enough for the
# binding's own `-llbug` to resolve against the pinned build -- and the single
# runtime path the test binary needs is passed through `-extldflags`, which the
# link step reads exactly once.
#
# One warning per binary survives, and it is the binding's: the `-L` it points
# at its own module directory does not exist in a pinned build. Silencing that
# would mean patching a module fixed by digest.
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
	CGO_LDFLAGS="-L$$LIB" \
	go test -tags ladybug -ldflags="-extldflags=-Wl,-rpath,$$LIB" $(ARGS) $(PKGS)

# lint-ladybug runs the dead-code check in the only configuration that can
# answer it. Under the default build the files behind the `ladybug` tag are
# not analysed at all, so every symbol whose caller lives in one of them looks
# unreferenced: measured at 20 findings, all of them false. With the tag they
# are read and the answer was zero, which is what makes this enforceable.
#
# It shares test-ladybug's environment because staticcheck type-checks cgo the
# same way the compiler does, and without the pinned library it cannot load
# the package it is being asked about.
lint-ladybug:
	@LIB="$$(scripts/fetch-ladybug.sh)"; \
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$$LIB" \
	CGO_LDFLAGS="-L$$LIB" \
	go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 \
		-checks=U1000 -tags ladybug $(PKGS)

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
