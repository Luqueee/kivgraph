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

// writePublishedSnapshot writes the snapshot into a generation directory.
//
// It goes through a temporary file in the same directory and a rename, even
// though a candidate directory is itself published by a rename: a reader that
// finds this file trusts its header, and a half-written header is exactly what
// a partially written file would offer.
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
	return nil
}

// loadPublishedSnapshot reads the snapshot a generation carries and proves it
// belongs there.
//
// The proof is the generation's own snapshot.sha256: the file's header repeats
// it, so a snapshot left behind by an earlier graph -- an incremental delta
// mutates a published generation in place and refreshes that digest -- is
// rejected rather than served. Every failure is one a caller can recover from
// by deriving the snapshot again, so none of them is wrapped as fatal.
func loadPublishedSnapshot(directory string) (*hotsnapshot.GraphSnapshot, error) {
	digestHex, err := os.ReadFile(filepath.Join(directory, snapshotFileName))
	if err != nil {
		return nil, fmt.Errorf("read generation digest: %w", err)
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
