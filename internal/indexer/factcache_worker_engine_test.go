package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// The modules of a worker build import the `typescript` package, which starts the
// native executable of the platform package beside it. A `pnpm install` that
// changes either leaves every byte of dist alone, so the identity of the worker
// has to look outside the build -- measured once, with the engine swapped in place
// under the same command and the next pass answering from the cache for a
// repository the new engine fails to load.
//
// Both layouts a worker is found in are built: the flat one of a bundle, where
// the packages are copies, and the pnpm one of a checkout, where `typescript` is
// a link into a store whose `@typescript` directory holds links to the platform
// packages.
type engineLayout struct {
	name  string
	build func(f workerFixture) (typescriptDir, platformDir string)
}

func engineLayouts() []engineLayout {
	return []engineLayout{
		{"flat", func(f workerFixture) (string, string) {
			modules := filepath.Join(f.root, "ts-worker", "node_modules")
			return filepath.Join(modules, "typescript"), filepath.Join(modules, "@typescript", "typescript-test")
		}},
		{"pnpm", func(f workerFixture) (string, string) {
			modules := filepath.Join(f.root, "ts-worker", "node_modules")
			store := filepath.Join(f.root, "ts-worker", "node_modules", ".pnpm", "typescript@1.0.0", "node_modules")
			engine := filepath.Join(store, "typescript")
			platform := filepath.Join(f.root, "ts-worker", "node_modules", ".pnpm", "typescript-test@1.0.0", "node_modules", "@typescript", "typescript-test")
			for _, directory := range []string{filepath.Join(store, "@typescript"), modules} {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					f.t.Fatal(err)
				}
			}
			// The platform package is a link beside `typescript`, and `typescript`
			// is a link under the worker's own node_modules.
			if err := os.MkdirAll(platform, 0o700); err != nil {
				f.t.Fatal(err)
			}
			if err := os.MkdirAll(engine, 0o700); err != nil {
				f.t.Fatal(err)
			}
			link(f, platform, filepath.Join(store, "@typescript", "typescript-test"))
			link(f, engine, filepath.Join(modules, "typescript"))
			return engine, platform
		}},
	}
}

func link(f workerFixture, target, name string) {
	f.t.Helper()
	if err := os.Symlink(target, name); err != nil {
		f.t.Skipf("cannot create a symbolic link here: %v", err)
	}
}

// engineFixture is a worker fixture with an engine installed in the given layout.
type engineFixture struct {
	workerFixture
	typescript, platform string
}

func newEngineFixture(t *testing.T, layout engineLayout) engineFixture {
	t.Helper()
	fixture := engineFixture{workerFixture: newWorkerFixture(t)}
	fixture.typescript, fixture.platform = layout.build(fixture.workerFixture)
	fixture.writeEngine("package.json", "{\"name\":\"typescript\",\"version\":\"1.0.0\"}\n")
	fixture.writeEngine("dist/api.js", "export const api = 1\n")
	fixture.writePlatform("lib/tsc", "native executable\n")
	fixture.writePlatform("lib/lib.d.ts", "interface Array<T> {}\n")
	return fixture
}

func (f engineFixture) writeEngine(relative, contents string) {
	f.t.Helper()
	writeFullFixture(f.t, filepath.Join(f.typescript, filepath.FromSlash(relative)), contents)
}

func (f engineFixture) writePlatform(relative, contents string) {
	f.t.Helper()
	writeFullFixture(f.t, filepath.Join(f.platform, filepath.FromSlash(relative)), contents)
}

func TestWorkerIdentityFollowsTheEngineItLoads(t *testing.T) {
	for _, layout := range engineLayouts() {
		for name, change := range map[string]func(engineFixture){
			"the executable is replaced": func(f engineFixture) {
				f.writePlatform("lib/tsc", "another native executable\n")
			},
			"a default library file is edited": func(f engineFixture) {
				f.writePlatform("lib/lib.d.ts", "interface Array<T> { extra: 1 }\n")
			},
			"a default library file is added": func(f engineFixture) {
				f.writePlatform("lib/lib.es2099.d.ts", "interface Added {}\n")
			},
			"a module of the typescript package is edited": func(f engineFixture) {
				f.writeEngine("dist/api.js", "export const api = 2\n")
			},
			"the version of the typescript package changes": func(f engineFixture) {
				f.writeEngine("package.json", "{\"name\":\"typescript\",\"version\":\"1.0.1\"}\n")
			},
			"the engine is removed": func(f engineFixture) {
				if err := os.RemoveAll(f.typescript); err != nil {
					f.t.Fatal(err)
				}
			},
		} {
			t.Run(layout.name+"/"+name, func(t *testing.T) {
				fixture := newEngineFixture(t, layout)
				before := fixture.fingerprint()
				change(fixture)
				if fixture.fingerprint() == before {
					t.Fatalf("the worker's identity ignored: %s", name)
				}
			})
		}
	}
}

// A platform package the engine does not use is still an engine that could be
// loaded: the side that hashes does not know which platform node runs on.
func TestWorkerIdentityFollowsAnyPlatformPackageBesideTheEngine(t *testing.T) {
	layout := engineLayouts()[0]
	fixture := newEngineFixture(t, layout)
	before := fixture.fingerprint()
	writeFullFixture(t, filepath.Join(filepath.Dir(fixture.platform), "typescript-other", "lib", "tsc"), "another platform\n")
	if fixture.fingerprint() == before {
		t.Fatal("a second platform package did not change the identity")
	}
}

// What an engine ships that the worker never runs must not throw the cache away:
// declarations and source maps of the typescript package.
func TestWorkerIdentityIgnoresWhatTheEngineNeverRuns(t *testing.T) {
	for _, layout := range engineLayouts() {
		t.Run(layout.name, func(t *testing.T) {
			fixture := newEngineFixture(t, layout)
			before := fixture.fingerprint()
			fixture.writeEngine("dist/api.d.ts", "export declare const api: number;\n")
			fixture.writeEngine("dist/api.d.mts", "export declare const api: number;\n")
			fixture.writeEngine("dist/api.d.cts", "export declare const api: number;\n")
			fixture.writeEngine("dist/api.js.map", "{\"version\":3}\n")
			if fixture.fingerprint() != before {
				t.Fatal("the worker's identity followed a file of the engine that the worker never runs")
			}
		})
	}
}

// The same engine is the same identity wherever it is installed and however it is
// linked: the checkout of a developer and the copy a bundle carries must agree.
func TestWorkerIdentityOfAnEngineIsItsContentAndNotItsLayout(t *testing.T) {
	layouts := engineLayouts()
	flat := newEngineFixture(t, layouts[0]).fingerprint()
	pnpm := newEngineFixture(t, layouts[1]).fingerprint()
	if flat != pnpm {
		t.Fatalf("the same engine has two identities:\nflat %q\npnpm %q", flat, pnpm)
	}
}

// No engine, an engine without a platform package and an engine with one are
// three different facts, and none of them is the identity of a worker whose
// engine was never looked for.
func TestWorkerIdentityNamesAnEngineThatIsMissingOrIncomplete(t *testing.T) {
	bare := newWorkerFixture(t).fingerprint()

	fixture := newEngineFixture(t, engineLayouts()[0])
	complete := fixture.fingerprint()
	if err := os.RemoveAll(filepath.Dir(fixture.platform)); err != nil {
		t.Fatal(err)
	}
	withoutPlatform := fixture.fingerprint()

	if bare == complete || bare == withoutPlatform || complete == withoutPlatform {
		t.Fatalf("identities collide: bare %q, complete %q, without a platform package %q", bare, complete, withoutPlatform)
	}
}

// Only a build output directory has an engine to look for. A script configured
// anywhere else keeps being identified by its own bytes, even with a typescript
// package nearby.
func TestWorkerIdentityDoesNotLookForAnEngineBesideAScriptThatIsNotABuild(t *testing.T) {
	fixture := newEngineFixture(t, engineLayouts()[0])
	other := filepath.Join(fixture.root, "scripts")
	entry := filepath.Join(other, "worker.js")
	writeFullFixture(t, entry, "console.log('worker')\n")
	writeFullFixture(t, filepath.Join(other, "node_modules", "typescript", "dist", "api.js"), "export const api = 1\n")
	options := FullOptions{TypeScriptWorker: filepath.Join(fixture.root, "node") + " " + entry}
	before := typeScriptWorkerFingerprint(options)
	writeFullFixture(t, filepath.Join(other, "node_modules", "typescript", "dist", "api.js"), "export const api = 2\n")
	if typeScriptWorkerFingerprint(options) != before {
		t.Fatal("an engine beside a script that is not a build was hashed")
	}
}

// Node finds the package by walking up, so the engine of the closest worker
// directory wins over one further up.
func TestWorkerIdentityTakesTheClosestEngine(t *testing.T) {
	fixture := newEngineFixture(t, engineLayouts()[0])
	before := fixture.fingerprint()
	writeFullFixture(t, filepath.Join(fixture.root, "node_modules", "typescript", "dist", "api.js"), "export const far = 1\n")
	if fixture.fingerprint() != before {
		t.Fatal("an engine further up the tree changed the identity of the closer one")
	}
}

// The watchers observe the worker through the path the configuration or the
// checkout rule gives them, which is relative to the working directory, and a
// pass through an absolute one. The engine of a monorepo can live above the
// working directory, and a walk that stops at it for one of them and not the other
// gives two identities to one worker: the profile is stale after every
// publication and is rebuilt until the attempts run out.
func TestWorkerIdentityOfAnEngineAboveTheWorkingDirectoryIsTheSameForARelativePath(t *testing.T) {
	root := testsupport.TempDir(t)
	program := filepath.Join(root, executable.Name("node"))
	writeFullFixture(t, program, "#!/bin/sh\necho node\n")
	if err := os.Chmod(program, 0o700); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(root, "packages", "worker")
	writeFullFixture(t, filepath.Join(checkout, "ts-worker", "dist", "facts-cli.js"), "import 'typescript'\n")
	engine := filepath.Join(root, "node_modules", "typescript", "dist", "api.js")
	writeFullFixture(t, engine, "export const api = 1\n")
	t.Chdir(checkout)

	relative := FullOptions{TypeScriptWorker: program + " " + filepath.Join("ts-worker", "dist", "facts-cli.js")}
	absolute := FullOptions{TypeScriptWorker: program + " " + filepath.Join(checkout, "ts-worker", "dist", "facts-cli.js")}
	before := typeScriptWorkerFingerprint(relative)
	if got, want := before, typeScriptWorkerFingerprint(absolute); got != want {
		t.Fatalf("one worker has two identities:\nrelative %q\nabsolute %q", got, want)
	}

	// The engine is what made the two agree, and not an absence both report.
	writeFullFixture(t, engine, "export const api = 2\n")
	if typeScriptWorkerFingerprint(relative) == before {
		t.Fatal("the engine above the working directory was not part of the identity")
	}
}
