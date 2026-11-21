package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"sort"
	"strings"
	"sync"
)

// The analyzer fingerprint is one value, and a difference in it says nothing about
// where the difference is. It is built in parts so that a pass that finds its
// entries refused can say which part of the analyzer moved: the executable, the Go
// toolchain, the Rust one, the TypeScript worker and so on. The parts are a
// diagnosis only. They never decide whether an entry is served, and the value is
// the hash of the same bytes in the same order it always was.
const (
	analyzerPartResolver   = "resolver"
	analyzerPartBinary     = "binary"
	analyzerPartGo         = "go"
	analyzerPartTypeScript = "typescript"
	analyzerPartPython     = "python"
	analyzerPartDart       = "dart"
	analyzerPartJava       = "java"
	analyzerPartCSharp     = "csharp"
	analyzerPartRust       = "rust"

	// analyzerPartUnrecorded stands for an entry written before the parts were
	// kept, which can only be said to differ.
	analyzerPartUnrecorded = "unrecorded"

	// analyzerPartDigits is how much of a part's hash is kept: enough to tell two
	// states apart in a diagnosis, short enough to carry in every entry.
	analyzerPartDigits = 16
)

// analyzerBuilder hashes what the fingerprint is made of, once for the whole value
// and once for the part it belongs to.
type analyzerBuilder struct {
	whole   hash.Hash
	parts   map[string]hash.Hash
	current hash.Hash
}

func newAnalyzerBuilder() *analyzerBuilder {
	return &analyzerBuilder{whole: sha256.New(), parts: make(map[string]hash.Hash)}
}

// part says which part the next lines belong to. A part can be left and come back:
// the bytes it receives are the ones written while it was selected, in order.
func (builder *analyzerBuilder) part(name string) {
	digest, found := builder.parts[name]
	if !found {
		digest = sha256.New()
		builder.parts[name] = digest
	}
	builder.current = digest
}

func (builder *analyzerBuilder) printf(format string, arguments ...any) {
	line := fmt.Sprintf(format, arguments...)
	_, _ = io.WriteString(builder.whole, line)
	_, _ = io.WriteString(builder.current, line)
}

func (builder *analyzerBuilder) fingerprint() string {
	return hex.EncodeToString(builder.whole.Sum(nil))
}

func (builder *analyzerBuilder) summary() map[string]string {
	parts := make(map[string]string, len(builder.parts))
	for name, digest := range builder.parts {
		parts[name] = hex.EncodeToString(digest.Sum(nil))[:analyzerPartDigits]
	}
	return parts
}

// recentAnalyzerParts remembers the parts of the fingerprints this process computed,
// by value. A fingerprint is the hash of its parts, so a value maps to one set of
// parts and the memory cannot go stale: it only lets the fact cache, which is handed
// the value an observation computed, name the parts without computing it again.
var recentAnalyzerParts struct {
	sync.Mutex
	order   []string
	byValue map[string]map[string]string
}

// recentAnalyzerPartsKept is how many fingerprints are remembered. A process holds
// one or two at a time; the bound keeps a long-lived one from collecting them.
const recentAnalyzerPartsKept = 8

func rememberAnalyzerParts(fingerprint string, parts map[string]string) {
	recentAnalyzerParts.Lock()
	defer recentAnalyzerParts.Unlock()
	if recentAnalyzerParts.byValue == nil {
		recentAnalyzerParts.byValue = make(map[string]map[string]string)
	}
	if _, known := recentAnalyzerParts.byValue[fingerprint]; !known {
		recentAnalyzerParts.order = append(recentAnalyzerParts.order, fingerprint)
		if len(recentAnalyzerParts.order) > recentAnalyzerPartsKept {
			delete(recentAnalyzerParts.byValue, recentAnalyzerParts.order[0])
			recentAnalyzerParts.order = recentAnalyzerParts.order[1:]
		}
	}
	recentAnalyzerParts.byValue[fingerprint] = parts
}

// analyzerPartsOf answers the parts of a fingerprint this process computed, nil when
// it did not.
func analyzerPartsOf(fingerprint string) map[string]string {
	recentAnalyzerParts.Lock()
	defer recentAnalyzerParts.Unlock()
	return recentAnalyzerParts.byValue[fingerprint]
}

// changedAnalyzerParts names the parts that differ between what an entry recorded
// and what this pass has. Either side without parts is `unrecorded`: nothing can be
// said about where the difference is.
func changedAnalyzerParts(recorded, current map[string]string) []string {
	if len(recorded) == 0 || len(current) == 0 {
		return []string{analyzerPartUnrecorded}
	}
	changed := make([]string, 0)
	for name, value := range current {
		if recorded[name] != value {
			changed = append(changed, name)
		}
	}
	for name := range recorded {
		if _, still := current[name]; !still {
			changed = append(changed, name)
		}
	}
	if len(changed) == 0 {
		// The values differ and no part does: nothing a part can name.
		return []string{analyzerPartUnrecorded}
	}
	sort.Strings(changed)
	return changed
}

// AnalyzerChangedNote renders which parts of the analyzer refused how many
// entries, as `binary:39,rust:12`, and is empty when none was refused.
func (report CacheReport) AnalyzerChangedNote() string {
	if len(report.AnalyzerChanged) == 0 {
		return ""
	}
	names := make([]string, 0, len(report.AnalyzerChanged))
	for name := range report.AnalyzerChanged {
		names = append(names, name)
	}
	sort.Strings(names)
	notes := make([]string, 0, len(names))
	for _, name := range names {
		notes = append(notes, fmt.Sprintf("%s:%d", name, report.AnalyzerChanged[name]))
	}
	return strings.Join(notes, ",")
}
