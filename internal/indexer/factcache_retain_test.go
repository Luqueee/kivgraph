package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// The address of an entry holds the content of its unit, so every edit writes a new
// entry and leaves the old one. Left to age alone the cache grew by the size of
// the unit on every edit, and nothing returned it for thirty days.
type retainedDirectory struct {
	t         *testing.T
	directory string
	count     int
}

func newRetainedDirectory(t *testing.T) *retainedDirectory {
	t.Helper()
	return &retainedDirectory{t: t, directory: testsupport.TempDir(t)}
}

// write admits an entry of unit, last used the given time ago, and answers its path.
func (d *retainedDirectory) write(unit string, ago time.Duration) string {
	d.t.Helper()
	d.count++
	body := encodeCacheEntry(cacheEntry{Version: cacheEntryVersion, Unit: unit, Analyzer: "analyzer", LocalAddress: fmt.Sprint(d.count)})
	// The facts follow the header and are what makes an entry large.
	body = append(body, []byte(strings.Repeat(" ", 20000))...)
	name := fmt.Sprintf("%064x%s", d.count, cacheEntrySuffix)
	path := filepath.Join(d.directory, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		d.t.Fatal(err)
	}
	used := time.Now().Add(-ago)
	if err := os.Chtimes(path, used, used); err != nil {
		d.t.Fatal(err)
	}
	return path
}

func (d *retainedDirectory) retain(keep int, admitted ...string) {
	d.t.Helper()
	protected := make(map[string]struct{}, len(admitted))
	for _, path := range admitted {
		protected[path] = struct{}{}
	}
	(&factCache{directory: d.directory, mode: CacheOn}).retain(keep, cacheEntriesPerUnitKept, protected)
}

func (d *retainedDirectory) retainCapped(keep, perUnit int, admitted ...string) {
	d.t.Helper()
	protected := make(map[string]struct{}, len(admitted))
	for _, path := range admitted {
		protected[path] = struct{}{}
	}
	(&factCache{directory: d.directory, mode: CacheOn}).retain(keep, perUnit, protected)
}

func (d *retainedDirectory) names() []string {
	d.t.Helper()
	items, err := os.ReadDir(d.directory)
	if err != nil {
		d.t.Fatal(err)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	slices.Sort(names)
	return names
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRetainKeepsTheNewestEntriesOfEachUnitAndNothingElse(t *testing.T) {
	d := newRetainedDirectory(t)
	var a, b []string
	for index := range 5 {
		a = append(a, d.write("typescript\x00one", time.Duration(5-index)*time.Hour)) // a[4] is the newest
	}
	for index := range 2 {
		b = append(b, d.write("typescript\x00two", time.Duration(2-index)*time.Hour))
	}
	d.retain(3)
	for index, path := range a {
		if want := index >= 2; exists(path) != want {
			t.Errorf("entry %d of five of one unit: exists = %t, want %t", index, exists(path), want)
		}
	}
	for index, path := range b {
		if !exists(path) {
			t.Errorf("entry %d of the unit that has no more than it keeps was removed", index)
		}
	}
}

// What the cache did not write is not its to remove, and an entry it cannot read
// the unit of is left to the age rule.
func TestRetainDoesNotTouchWhatItCannotAttribute(t *testing.T) {
	d := newRetainedDirectory(t)
	var entries []string
	for range 4 {
		entries = append(entries, d.write("typescript\x00one", time.Hour))
	}
	// The header of an entry that names no unit: the magic, the structure and a
	// unit of no byte.
	emptyUnit := cacheEntryMagic + string(entryFingerprint[:]) + "\x00"
	other := map[string]string{
		"notes.txt":   "mine",
		"report.json": `{"unit":"typescript\u0000one"}`,
		strings.Repeat("a", 63) + cacheEntrySuffix: `{"unit":"typescript\u0000one"}`,
		".entry-1.tmp": `{"unit":"typescript\u0000one"}`,
		strings.Repeat("c", 64) + cacheEntrySuffix: "KVFE",
		strings.Repeat("d", 64) + cacheEntrySuffix: "not an entry at all",
		strings.Repeat("e", 64) + cacheEntrySuffix: emptyUnit,
	}
	old := time.Now().Add(-100 * time.Hour)
	for name, content := range other {
		path := filepath.Join(d.directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	d.retain(3)
	for name := range other {
		if !exists(filepath.Join(d.directory, name)) {
			t.Errorf("%s was removed by the retention", name)
		}
	}
	survivors := 0
	for _, path := range entries {
		if exists(path) {
			survivors++
		}
	}
	if survivors != 3 {
		t.Errorf("%d entries of the unit survive, want 3", survivors)
	}
}

// A header that claims a unit beyond what a header may is not believed: a damaged
// length must not make retention allocate what the file does not hold.
func TestRetainDoesNotCountAnEntryWhoseUnitIsBeyondWhatAHeaderMayClaim(t *testing.T) {
	d := newRetainedDirectory(t)
	long := "typescript\x00" + strings.Repeat("p", cacheMaximumUnitBytes)
	var paths []string
	for range 5 {
		paths = append(paths, d.write(long, time.Hour))
	}
	d.retain(1)
	for _, path := range paths {
		if !exists(path) {
			t.Fatal("an entry whose unit was not read was removed")
		}
	}
}

func TestRetainNeverRemovesWhatThePassJustAdmitted(t *testing.T) {
	d := newRetainedDirectory(t)
	var paths []string
	for index := range 5 {
		paths = append(paths, d.write("typescript\x00one", time.Duration(5-index)*time.Hour))
	}
	// The oldest is what the pass admitted, whatever the clock says.
	d.retain(2, paths[0])
	if !exists(paths[0]) {
		t.Fatal("an entry the pass admitted was removed")
	}
	if !exists(paths[4]) || !exists(paths[3]) {
		t.Fatal("the newest entries were removed")
	}
	if exists(paths[1]) || exists(paths[2]) {
		t.Fatal("entries beyond what is kept survived")
	}
}

// Serving an entry is using it: an old entry that was just served outranks a newer
// one nobody asked for, which is what going back to a branch needs.
func TestRetainPrefersTheEntryThatWasUsedLast(t *testing.T) {
	d := newRetainedDirectory(t)
	oldest := d.write("typescript\x00one", 4*time.Hour)
	middle := d.write("typescript\x00one", 3*time.Hour)
	recent := d.write("typescript\x00one", 2*time.Hour)
	newest := d.write("typescript\x00one", time.Hour)
	now := time.Now()
	if err := os.Chtimes(oldest, now, now); err != nil {
		t.Fatal(err)
	}
	d.retain(3)
	if !exists(oldest) || !exists(recent) || !exists(newest) || exists(middle) {
		t.Fatalf("after serving the oldest the directory holds %q, want the middle one gone", d.names())
	}
}

// The identity of a unit includes the resolution it was analysed in, so the profiles
// that share a directory do not spend one another's entries.
func TestRetainCountsEveryResolutionOfAUnitApart(t *testing.T) {
	d := newRetainedDirectory(t)
	var paths []string
	for _, resolution := range []string{"backend", "frontend"} {
		for index := range 3 {
			paths = append(paths, d.write("typescript\x00one\x00resolution\x00"+resolution, time.Duration(3-index)*time.Hour))
		}
	}
	d.retain(3)
	for _, path := range paths {
		if !exists(path) {
			t.Fatal("an entry of one resolution was removed to make room for another's")
		}
	}
}

// A retention that kept nothing would empty the cache.
func TestRetainKeepsAtLeastOneEntry(t *testing.T) {
	d := newRetainedDirectory(t)
	path := d.write("typescript\x00one", time.Hour)
	d.write("typescript\x00one", 2*time.Hour)
	d.retain(0)
	d.retain(-1)
	if !exists(path) || len(d.names()) != 2 {
		t.Fatalf("a retention of nothing removed entries: %q", d.names())
	}
}

// Through a pass: every edit admits an entry, and the unit keeps the newest three.
// The revert that the entry before the last edit serves is a hit; the one the
// retention removed is not.
func TestFactCacheDoesNotGrowWithEveryEdit(t *testing.T) {
	fixture := newCachedFixture(t)
	source := filepath.Join(fixture.root, "fixture.go")
	contents := func(version int) string {
		return fmt.Sprintf("package fixture\n\nfunc Greeting() string { return %q }\n", fmt.Sprint("hello ", version))
	}
	index := func() CacheReport {
		t.Helper()
		_, report, err := Full(context.Background(), FullOptions{
			Repositories:      []workspace.Repository{fixture.repository},
			SyntheticWorkFile: fixture.workFile,
			CacheMode:         fixture.mode,
			CacheDirectory:    fixture.cache,
		})
		if err != nil {
			t.Fatalf("Full() error = %v", err)
		}
		return report.Cache
	}
	entries := func() int {
		t.Helper()
		found, err := filepath.Glob(filepath.Join(fixture.cache, "*"+cacheEntrySuffix))
		if err != nil {
			t.Fatal(err)
		}
		return len(found)
	}
	// Distinct modification times, so that the newest is not a matter of the clock.
	for version := 1; version <= 6; version++ {
		writeFullFixture(t, source, contents(version))
		if report := index(); report.Misses != 1 {
			t.Fatalf("version %d cache = %+v, want one miss", version, report)
		}
		time.Sleep(20 * time.Millisecond)
		if got := entries(); got > cacheEntriesKept {
			t.Fatalf("after %d edits the cache holds %d entries of one unit, want at most %d", version, got, cacheEntriesKept)
		}
	}
	if got := entries(); got != cacheEntriesKept {
		t.Fatalf("after six edits the cache holds %d entries, want %d", got, cacheEntriesKept)
	}

	// Back to the version before the last, and to one the retention removed.
	writeFullFixture(t, source, contents(5))
	if report := index(); report.Hits != 1 {
		t.Fatalf("going back to the previous version cache = %+v, want a hit", report)
	}
	writeFullFixture(t, source, contents(1))
	if report := index(); report.Hits != 0 || report.Misses != 1 {
		t.Fatalf("going back to a version beyond what is kept cache = %+v, want a miss", report)
	}
}

// Raising the version of a package changed the resolution of every unit that reads
// the registry, and each old resolution kept its entries for thirty days: the
// identity rule counts a resolution apart, so only a cap on the unit bounds how
// many it can leave behind.
func TestRetainCapsAUnitAcrossItsResolutions(t *testing.T) {
	d := newRetainedDirectory(t)
	type kept struct {
		path string
		want bool
	}
	var entries []kept
	age := 8 // hours, so that the oldest of the eight is the first written
	for _, resolution := range []string{"one", "two", "three", "four"} {
		for range 2 {
			path := d.write("typescript\x00pkg\x00resolution\x00"+resolution, time.Duration(age)*time.Hour)
			entries = append(entries, kept{path: path, want: age <= cacheEntriesPerUnitKept})
			age--
		}
	}
	other := d.write("typescript\x00another\x00resolution\x00one", 100*time.Hour)
	d.retain(cacheEntriesKept)
	for index, entry := range entries {
		if exists(entry.path) != entry.want {
			t.Errorf("entry %d of eight of one unit: exists = %t, want %t", index, exists(entry.path), entry.want)
		}
	}
	if !exists(other) {
		t.Error("the cap on one unit removed an entry of another")
	}
}

func TestRetainAppliesTheRuleOfAnIdentityBeforeTheCapOfTheUnit(t *testing.T) {
	d := newRetainedDirectory(t)
	var first, second []string
	for index := range 5 {
		first = append(first, d.write("typescript\x00pkg\x00resolution\x00a", time.Duration(10-index)*time.Hour))
		second = append(second, d.write("typescript\x00pkg\x00resolution\x00b", time.Duration(10-index)*time.Hour))
	}
	d.retain(cacheEntriesKept)
	for _, group := range [][]string{first, second} {
		survivors := 0
		for _, path := range group {
			if exists(path) {
				survivors++
			}
		}
		if survivors != cacheEntriesKept {
			t.Errorf("%d entries of one resolution survive, want the %d its identity keeps", survivors, cacheEntriesKept)
		}
	}
}

// A cap below what one identity keeps would undo that rule.
func TestRetainNeverCapsAUnitBelowWhatOneIdentityKeeps(t *testing.T) {
	d := newRetainedDirectory(t)
	for index := range 4 {
		d.write("typescript\x00pkg\x00resolution\x00a", time.Duration(4-index)*time.Hour)
	}
	d.retainCapped(3, 1)
	if got := len(d.names()); got != 3 {
		t.Fatalf("a cap of one left %d entries of one identity, want the 3 it keeps", got)
	}
}

func TestRetainNeverRemovesAnAdmittedEntryToHonourTheCap(t *testing.T) {
	d := newRetainedDirectory(t)
	var paths []string
	for index, resolution := range []string{"a", "b", "c", "d"} {
		for range 2 {
			paths = append(paths, d.write("typescript\x00pkg\x00resolution\x00"+resolution, time.Duration(100-index)*time.Hour))
		}
	}
	// The pass admitted the oldest, whatever the clock says.
	d.retain(cacheEntriesKept, paths[0])
	if !exists(paths[0]) {
		t.Fatal("an entry the pass admitted was removed by the cap of its unit")
	}
	if got := len(d.names()); got < cacheEntriesPerUnitKept || got > cacheEntriesPerUnitKept+1 {
		t.Fatalf("%d entries of the unit survive, want the cap of %d and at most the admitted one besides", got, cacheEntriesPerUnitKept)
	}
}

// The cap on the entries of a unit across its resolutions rests on one thing: the
// identity of every unit ends in the separator and the resolution, whatever kind of
// unit it is. A kind whose identity did not would grow with every change of its
// registry, which is every edit for the semantic languages, and nothing else would
// say so. The registry of each kind is a different string here, so that a kind
// that read another's would be seen to.
func TestEveryUnitKindCarriesItsResolutionInItsIdentity(t *testing.T) {
	repository := workspace.Repository{Name: "repo", RealPath: "/work/repo"}
	units := map[string]analysisUnit{
		"go": {kind: unitGo, repository: repository, module: goworkspace.Module{ModulePath: "example.com/repo", RootPath: "/work/repo"}},
		"typescript": {kind: unitTypeScript, repository: repository, pkg: typeScriptPackageUnit{
			repository: repository, packageValue: workspace.TypeScriptPackage{Name: "pkg", ManifestPath: "/work/repo/package.json"},
		}},
		"rust":    {kind: unitRust, repository: repository, rust: rustWorkspaceUnit{workspace: workspace.CargoWorkspace{RootPath: "/work/repo"}}},
		"python":  {kind: unitSemantic, repository: repository, language: facts.LanguagePython},
		"dart":    {kind: unitSemantic, repository: repository, language: facts.LanguageDart},
		"no kind": {kind: unitUnspecified, repository: repository},
	}
	registries := func(suffix string) *factCache {
		return &factCache{
			goRegistry: "go-" + suffix, rustRegistry: "rust-" + suffix,
			semanticRegistry: "semantic-" + suffix, typeScriptRegistry: "typescript-" + suffix,
		}
	}
	before, after := registries("before"), registries("after")

	// What each kind reads as its resolution: its own registry, and nobody else's.
	own := map[string]string{
		"go": "go-before", "typescript": "typescript-before", "rust": "rust-before",
		"python": "semantic-before", "dart": "semantic-before", "no kind": "none",
	}
	logical := make(map[string]string, len(units))
	for name, unit := range units {
		identity := before.identity(unit)
		if strings.Count(identity, cacheResolutionSeparator) != 1 {
			t.Errorf("%s: identity %q does not carry its resolution once", name, identity)
		}
		if !strings.HasSuffix(identity, cacheResolutionSeparator+own[name]) {
			t.Errorf("%s: identity %q does not end in its own resolution %q", name, identity, own[name])
		}
		if got, want := logicalUnit(identity), unitIdentity(unit); got != want {
			t.Errorf("%s: the unit of %q is %q, want %q", name, identity, got, want)
		}
		// A change of registry is another identity of the same unit: that is what the
		// cap counts together.
		changed := after.identity(unit)
		// The kind that is no kind has no registry: analyseUnit refuses it, and its
		// resolution is a constant.
		if unit.kind == unitUnspecified {
			if !strings.HasSuffix(identity, cacheResolutionSeparator+"none") {
				t.Errorf("%s: identity %q does not end in the constant resolution", name, identity)
			}
		} else if changed == identity {
			t.Errorf("%s: another registry left the identity as it was", name)
		}
		if logicalUnit(changed) != logicalUnit(identity) {
			t.Errorf("%s: another registry made another unit of %q and %q", name, identity, changed)
		}
		logical[name] = logicalUnit(identity)
	}

	// And two units are not one: the languages that share the semantic kind are
	// capped apart.
	seen := make(map[string]string, len(logical))
	for name, unit := range logical {
		if other, clash := seen[unit]; clash {
			t.Errorf("%s and %s are the same unit %q, so they would share one cap", name, other, unit)
		}
		seen[unit] = name
	}
}
