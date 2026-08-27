package rebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/metrics"
)

// PublishedSnapshotFileName is the HotSnapshot a generation carries, next to
// its database and its digest.
//
// Before this existed, a generation carried only the canonical graph and every
// server derived the snapshot from it: 1.003 MB allocated and ~1,05 GB of peak
// per install, once per published generation, in every process. See ADR 0045.
const PublishedSnapshotFileName = "snapshot.kvsnap"

// PublishedSnapshotDigestFileName records the digest of the graph the
// published snapshot contains. It is what proves the file belongs to this
// generation.
//
// It is deliberately not snapshot.sha256. That one digests the canonical table
// counts, and counts cannot tell two graphs of the same shape apart: measured
// on `workspace`, two indexings whose graphs differed in 288 rows produced a
// byte-identical snapshot.sha256. A file proven only against counts is a file
// that can be accepted for a graph it does not contain. The counts digest
// keeps its own job, which is Rollback's cheap check that a destination
// database still holds what it recorded. See ADR 0061.
//
// A generation published before this file existed carries none, which is not a
// defect: the reader cannot prove the snapshot and derives the graph from the
// canonical store exactly as it always did.
const PublishedSnapshotDigestFileName = "snapshot.content.sha256"

// writePublishedSnapshot writes the snapshot into a generation directory,
// together with the record that proves which graph it holds.
//
// It goes through a temporary file in the same directory and a rename, even
// though a candidate directory is itself published by a rename: a reader that
// finds this file trusts its header, and a half-written header is exactly what
// a partially written file would offer.
//
// The record is written last, so its presence implies the file. The reverse
// order would let a reader find a proof for a snapshot that is not there yet,
// and the two failures are told apart on purpose.
func writePublishedSnapshot(directory string, snapshot *hotsnapshot.GraphSnapshot, digestHex string) error {
	contentDigest, err := decodeSnapshotDigest(digestHex)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, PublishedSnapshotFileName+".*")
	if err != nil {
		return fmt.Errorf("create snapshot file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		temporary.Close()
		os.Remove(temporaryPath)
	}()
	if _, err := hotsnapshot.WriteSnapshot(temporary, snapshot, contentDigest); err != nil {
		return fmt.Errorf("write snapshot file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync snapshot file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close snapshot file: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return fmt.Errorf("chmod snapshot file: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, PublishedSnapshotFileName)); err != nil {
		return fmt.Errorf("install snapshot file: %w", err)
	}
	record := filepath.Join(directory, PublishedSnapshotDigestFileName)
	if err := os.WriteFile(record, []byte(digestHex+"\n"), 0o600); err != nil {
		return fmt.Errorf("record the published snapshot digest: %w", err)
	}
	return nil
}

// loadPublishedSnapshot reads the snapshot a generation carries and proves it
// belongs there.
//
// The proof is the digest of the graph the generation recorded: the file's
// header repeats it, so a snapshot holding a different graph is rejected
// rather than served. It is not snapshot.sha256, which digests table counts
// and therefore cannot distinguish two graphs of the same shape -- the reason
// this record exists at all.
//
// Every failure here is one a caller can recover from by deriving the snapshot
// again, so none of them is wrapped as fatal, and a generation published
// before the record existed simply has none.
func loadPublishedSnapshot(directory string) (*hotsnapshot.GraphSnapshot, error) {
	digestHex, err := os.ReadFile(filepath.Join(directory, PublishedSnapshotDigestFileName))
	switch {
	case errors.Is(err, os.ErrNotExist):
		// A generation published before this record existed. Nothing is
		// wrong with it: it carries the definitive graph, and a reader
		// that cannot prove the snapshot derives it. Saying this is a
		// failure would put doctor in red on every install that upgrades.
		return nil, fmt.Errorf("%w: %s", ErrNoRecordedGraphDigest, PublishedSnapshotDigestFileName)
	case err != nil:
		return nil, fmt.Errorf("read the recorded graph digest: %w", err)
	}
	contentDigest, err := decodeSnapshotDigest(strings.TrimSpace(string(digestHex)))
	if err != nil {
		return nil, err
	}
	data, release, err := mapFile(filepath.Join(directory, PublishedSnapshotFileName))
	if err != nil {
		return nil, fmt.Errorf("read published snapshot: %w", err)
	}
	snapshot, err := hotsnapshot.MapSnapshot(data, contentDigest)
	if err != nil {
		release()
		return nil, err
	}
	// The mapping outlives this call, because the snapshot reads its string
	// values out of it instead of copying them: some fifty megabytes on a real
	// corpus, and two processes reading the same generation share those pages.
	//
	// It is released when the snapshot becomes unreachable, which is the only
	// moment nothing can name those bytes. A query holding a snapshot keeps it
	// reachable, and every string the snapshot hands out of a borrowed arena is a
	// copy, so no answer can outlive the pages it was read from. The cleanup
	// closes over the release function and never over the snapshot, which would
	// keep it alive forever.
	runtime.AddCleanup(snapshot, func(release func()) { release() }, release)
	return snapshot, nil
}

// LoadOrBuildSnapshot answers with the generation's published snapshot when it
// can be trusted, and derives it from the canonical graph when it cannot.
//
// The fallback is the whole point: a generation always carries the definitive
// graph, so a missing, foreign, stale or corrupt snapshot file costs a rebuild
// and never an answer. The report says which happened, because a server that
// silently derives every time would look exactly like one that never had to.
func LoadOrBuildSnapshot(ctx context.Context, options BuildSnapshotOptions) (*hotsnapshot.GraphSnapshot, SnapshotReport, error) {
	directory := filepath.Dir(options.DatabasePath)
	start := time.Now()
	snapshot, err := loadPublishedSnapshot(directory)
	if err == nil {
		metadata := snapshot.Metadata()
		counts := metadata.Counts
		report := SnapshotReport{
			SnapshotID: metadata.ID,
			Version:    metadata.Version,
			Passed:     true,
			Loaded:     true,
			Stats: SnapshotStats{
				Repositories: int(counts.Repositories), Packages: int(counts.Packages),
				Files: int(counts.Files), Symbols: int(counts.Symbols),
				Evidence: int(counts.Evidence), Edges: int(counts.Edges),
				PackageEdges: int(counts.PackageEdges), Unresolved: int(counts.Unresolved),
			},
		}
		if options.Metrics != nil {
			databaseBytes := int64(0)
			if info, statErr := os.Stat(options.DatabasePath); statErr == nil {
				databaseBytes = info.Size()
			}
			options.Metrics.ObserveSnapshot(metrics.SnapshotObservation{
				ID: metadata.ID, CreatedAt: metadata.CreatedAt,
				BuildDuration: time.Since(start), Bytes: databaseBytes,
			})
		}
		return snapshot, report, nil
	}
	snapshot, report, buildErr := BuildSnapshot(ctx, options)
	if buildErr != nil {
		return nil, report, buildErr
	}
	report.LoadRefused = err.Error()
	return snapshot, report, nil
}

// decodeSnapshotDigest turns a generation's hexadecimal digest into the raw
// bytes a snapshot header carries. A digest that is not one is not a mismatch
// to report later: nothing can be proven against it, so it fails here.
func decodeSnapshotDigest(value string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return digest, fmt.Errorf("%w: digest %q is not hexadecimal", errInvalidSnapshotDigest, value)
	}
	if len(decoded) != sha256.Size {
		return digest, fmt.Errorf("%w: digest %q is %d bytes, want %d",
			errInvalidSnapshotDigest, value, len(decoded), sha256.Size)
	}
	copy(digest[:], decoded)
	return digest, nil
}

var errInvalidSnapshotDigest = errors.New("invalid generation digest")

// PublishedSnapshotInfo describes the snapshot a generation carries.
type PublishedSnapshotInfo struct {
	Path    string
	Bytes   int64
	ID      uint64
	Symbols int
}

// ErrNoPublishedSnapshot says a generation carries no snapshot, which is not a
// defect: one published before the file existed carries none, and a reader
// derives the graph exactly as it always did. A file that is there and cannot be
// used is a different answer, and gets a different error.
var ErrNoPublishedSnapshot = errors.New("the generation carries no published snapshot")

// ErrNoRecordedGraphDigest says a generation carries a snapshot but not the
// digest of the graph it should hold, which is what every generation published
// before ADR 0061 looks like.
//
// It is the same class of answer as absence, and for the same reason: nothing
// is wrong, the definitive graph is there, and a reader that cannot prove the
// file derives the snapshot instead. Counting it as a failure would mark
// doctor red on every install the moment it upgrades, which is exactly how a
// real failure stops being noticed.
var ErrNoRecordedGraphDigest = errors.New("the generation records no digest for its published snapshot")

// InspectPublishedSnapshot answers what a generation's published snapshot is, or
// why a server would have to derive the graph instead of reading it.
//
// It exists so a tool can say which of the two is happening. Nothing did, and
// the cost of that silence is measured: twice while this was being built, a
// server derived the whole graph -- 1003 MB and 1.7 s per generation -- and
// every suite stayed green, because both routes produce the same snapshot and
// only memory tells them apart.
func InspectPublishedSnapshot(directory string) (PublishedSnapshotInfo, error) {
	path := filepath.Join(directory, PublishedSnapshotFileName)
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return PublishedSnapshotInfo{Path: path}, ErrNoPublishedSnapshot
	case err != nil:
		return PublishedSnapshotInfo{Path: path}, err
	}
	snapshot, err := loadPublishedSnapshot(directory)
	if err != nil {
		return PublishedSnapshotInfo{Path: path, Bytes: info.Size()}, err
	}
	metadata := snapshot.Metadata()
	return PublishedSnapshotInfo{
		Path:    path,
		Bytes:   info.Size(),
		ID:      metadata.ID,
		Symbols: int(metadata.Counts.Symbols),
	}, nil
}
