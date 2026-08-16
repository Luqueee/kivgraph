package indexing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
)

// FollowInterval is how often a follower asks the generation store which
// generation is published. The question costs one read of the CURRENT pointer,
// and the answer only changes when something else publishes, so the interval
// trades a negligible cost for a bounded staleness a person will not notice.
const FollowInterval = 2 * time.Second

// FollowOptions configures Follow.
type FollowOptions struct {
	// Root is the directory that holds the generation store.
	Root string
	// Store selects the generation layout; the zero value is the default.
	Store generation.Config
	// Interval overrides FollowInterval.
	Interval time.Duration
	// OnPublish, when set, is called with each generation this follower
	// installs. It must not block.
	OnPublish func(uint64)
	// OnError, when set, receives a failure the follower absorbed. A
	// follower never stops on a failed generation: the published one keeps
	// answering queries, and the next tick tries again.
	OnError func(error)
}

// Follow keeps a snapshot store on the generation the store root publishes.
//
// A server loads the published HotSnapshot once, at startup. Anything that
// publishes afterwards -- `kivgraph index --full` in another terminal, or
// another process entirely -- leaves that server answering from a graph that
// no longer exists on disk, with no way to tell. Follow closes that gap by
// reading the CURRENT pointer and rebuilding only when the identifier moves.
//
// It never coordinates with other publishers: SnapshotStore.Publish only
// accepts a strictly newer generation, so a follower that loses the race to
// an in-process index simply observes the newer identifier on its next tick.
//
// Follow blocks until ctx is done.
func Follow(ctx context.Context, store *hotsnapshot.SnapshotStore, options FollowOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store == nil {
		return errors.New("follow published generation: snapshot store is required")
	}
	if options.Root == "" {
		return errors.New("follow published generation: root is required")
	}
	interval := options.Interval
	if interval <= 0 {
		interval = FollowInterval
	}
	generations, err := generation.New(options.Root, storeConfigOrDefault(options.Store))
	if err != nil {
		return fmt.Errorf("open generation store: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	reportedRewind := uint64(0)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			result, err := followOnce(ctx, store, generations)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				report(options.OnError, err)
				continue
			}
			if result.Published != 0 {
				reportPublished(options.OnPublish, result.Published)
			}
			// A store that publishes an older generation than the one
			// being served has been rewound -- discarded and rebuilt.
			// Publish only accepts a newer identifier, so this server
			// will never install anything again; saying so once is the
			// difference between a restart and an afternoon.
			if result.Rewound != 0 && result.Rewound != reportedRewind {
				reportedRewind = result.Rewound
				report(options.OnError, fmt.Errorf(
					"generation store was rewound to %d while serving %d: restart to follow it again",
					result.Rewound, store.Load().Metadata().ID))
			}
		}
	}
}

// followResult is what one poll of the generation store concluded.
type followResult struct {
	// Published is the generation this poll installed, or zero.
	Published uint64
	// Rewound is the active generation when it is older than the one being
	// served, or zero. It cannot be installed and never will be.
	Rewound uint64
}

// followOnce publishes the active generation when it is newer than the one
// being served, and reports what it concluded.
func followOnce(
	ctx context.Context,
	store *hotsnapshot.SnapshotStore,
	generations *generation.Store,
) (followResult, error) {
	active, err := generations.Current(ctx)
	if err != nil {
		if errors.Is(err, generation.ErrNoCurrent) {
			// Nothing has ever been published, or everything was
			// discarded. A server keeps whatever it holds: the store
			// publishes generations, it never retracts one.
			return followResult{}, nil
		}
		return followResult{}, fmt.Errorf("read active generation: %w", err)
	}
	activeID, err := parseSnapshotID(active.ID)
	if err != nil {
		return followResult{}, err
	}
	served := store.Load()
	if !needsPublication(served, activeID) {
		if served != nil && served.Metadata().ID > activeID {
			return followResult{Rewound: activeID}, nil
		}
		return followResult{}, nil
	}
	snapshot, report, err := rebuild.LoadOrBuildSnapshot(ctx, rebuild.BuildSnapshotOptions{
		DatabasePath: active.DatabasePath,
		SnapshotID:   activeID,
	})
	// From here the build's inputs are unreachable whatever happens next,
	// including the lost race below, where the whole snapshot is dropped too.
	defer rebuild.ReturnBuildMemory()
	if err != nil {
		return followResult{}, fmt.Errorf("build published snapshot %q: %w", active.ID, err)
	}
	if !report.Passed {
		return followResult{}, fmt.Errorf("build published snapshot %q did not pass", active.ID)
	}
	if err := store.Publish(snapshot); err != nil {
		if errors.Is(err, hotsnapshot.ErrSnapshotGeneration) {
			// Another publisher installed this generation, or a newer
			// one, while this snapshot was being built. Its work stands.
			return followResult{}, nil
		}
		return followResult{}, fmt.Errorf("publish snapshot %q: %w", active.ID, err)
	}
	return followResult{Published: activeID}, nil
}

// needsPublication reports whether the active generation is newer than the one
// being served.
//
// The comparison is against the store, never against a counter the follower
// keeps: another publisher -- an in-process index_project -- installs
// generations through the same store, and a follower that remembered its own
// last answer would rebuild what is already being served.
func needsPublication(served *hotsnapshot.GraphSnapshot, activeID uint64) bool {
	if served == nil {
		return true
	}
	return served.Metadata().ID < activeID
}

func storeConfigOrDefault(config generation.Config) generation.Config {
	if config.IsZero() {
		return generation.DefaultConfig()
	}
	return config
}

func report(sink func(error), err error) {
	if sink == nil || err == nil {
		return
	}
	sink(err)
}

func reportPublished(sink func(uint64), id uint64) {
	if sink == nil {
		return
	}
	sink(id)
}
