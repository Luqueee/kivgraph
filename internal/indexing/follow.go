package indexing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
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
// publishes afterwards -- `ladygraph index --full` in another terminal, or
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
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			published, err := followOnce(ctx, store, generations)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				report(options.OnError, err)
				continue
			}
			if published != 0 {
				reportPublished(options.OnPublish, published)
			}
		}
	}
}

// followOnce publishes the active generation when it is newer than the one
// being served, and answers the identifier it installed. Zero means the served
// generation is already the published one: the common answer, and the reason
// the caller can log every non-zero result without flooding anyone.
func followOnce(
	ctx context.Context,
	store *hotsnapshot.SnapshotStore,
	generations *generation.Store,
) (uint64, error) {
	active, err := generations.Current(ctx)
	if err != nil {
		if errors.Is(err, generation.ErrNoCurrent) {
			// Nothing has ever been published. A server started before
			// the first index keeps answering INDEX_NOT_READY until it
			// has something to serve.
			return 0, nil
		}
		return 0, fmt.Errorf("read active generation: %w", err)
	}
	activeID, err := parseSnapshotID(active.ID)
	if err != nil {
		return 0, err
	}
	if served := store.Load(); served != nil && served.Metadata().ID >= activeID {
		return 0, nil
	}
	snapshot, report, err := rebuild.BuildSnapshot(ctx, rebuild.BuildSnapshotOptions{
		DatabasePath: active.DatabasePath,
		SnapshotID:   activeID,
	})
	if err != nil {
		return 0, fmt.Errorf("build published snapshot %q: %w", active.ID, err)
	}
	if !report.Passed {
		return 0, fmt.Errorf("build published snapshot %q did not pass", active.ID)
	}
	if err := store.Publish(snapshot); err != nil {
		if errors.Is(err, hotsnapshot.ErrSnapshotGeneration) {
			// Another publisher installed this generation, or a newer
			// one, while this snapshot was being built. Its work stands.
			return 0, nil
		}
		return 0, fmt.Errorf("publish snapshot %q: %w", active.ID, err)
	}
	return activeID, nil
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
