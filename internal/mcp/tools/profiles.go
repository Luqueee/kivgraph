package tools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// ProfileNames is a comparable in-memory list so existing result rows remain
// comparable in tests while the wire shape is the required JSON array.
type ProfileNames string

func profileNames(names ...string) ProfileNames { return ProfileNames(strings.Join(names, "\x00")) }

func (names ProfileNames) append(name string) ProfileNames {
	if names == "" {
		return ProfileNames(name)
	}
	return names + ProfileNames("\x00"+name)
}

// MarshalJSON emits the public array shape.
func (names ProfileNames) MarshalJSON() ([]byte, error) {
	if names == "" {
		return []byte("[]"), nil
	}
	return json.Marshal(strings.Split(string(names), "\x00"))
}

// ProfileSetSnapshotID compresses the profile/generation vector into the
// immutable identity the existing cursor already pins. A global offset over
// canonical profile order then needs no per-profile positions on the wire.
func ProfileSetSnapshotID(profiles []ProfileSnapshot) uint64 {
	canonical := append([]ProfileSnapshot(nil), profiles...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].Name < canonical[j].Name })
	hash := sha256.New()
	var encoded [8]byte
	for _, profile := range canonical {
		binary.BigEndian.PutUint64(encoded[:], uint64(len(profile.Name)))
		_, _ = hash.Write(encoded[:])
		_, _ = hash.Write([]byte(profile.Name))
		binary.BigEndian.PutUint64(encoded[:], profile.SnapshotID)
		_, _ = hash.Write(encoded[:])
	}
	return binary.BigEndian.Uint64(hash.Sum(nil)[:8])
}

// RequireStableKeyProfile prevents a stable key from being resolved through a
// movable default pointer once the installation contains several profiles.
func RequireStableKeyProfile(profileCount int, stableKey string, requested []string) error {
	if profileCount > 1 && strings.TrimSpace(stableKey) != "" && (len(requested) != 1 || requested[0] == "*") {
		return NewToolError(CodeInvalidArgument,
			"stable_key requires exactly one named profile when more than one profile exists")
	}
	return nil
}

func resolveSingleProfile(
	store *hotsnapshot.SnapshotStore,
	requested []string,
	stableKey string,
) (*hotsnapshot.SnapshotStore, string, int, error) {
	if store == nil {
		return nil, "", 0, ErrIndexNotReady()
	}
	count := store.ProfileCount()
	if err := RequireStableKeyProfile(count, stableKey, requested); err != nil {
		return nil, "", count, err
	}
	selected, err := store.ResolveProfiles(requested)
	if err != nil {
		return nil, "", count, WrapToolError(CodeInvalidArgument, err.Error(), err)
	}
	if len(selected) != 1 {
		return nil, "", count, NewToolError(CodeInvalidArgument,
			"this query requires one profile; multi-profile union is not available for this operation")
	}
	return selected[0].Store, selected[0].Name, count, nil
}

func scopeResponse[T any](response *Response[T], profile string, profileCount int) {
	if response != nil && profileCount > 1 {
		response.Profile = profile
	}
}

func addCoverage(total *Coverage, next Coverage) {
	total.Exact += next.Exact
	total.Candidate += next.Candidate
	total.UnresolvedRelated += next.UnresolvedRelated
	total.PackageLevel += next.PackageLevel
}

func mergeCompleteness(total *Completeness, next *Completeness) {
	if total == nil || next == nil || next.Verdict != VerdictLowerBound {
		return
	}
	total.Verdict = VerdictLowerBound
	total.BlindSpots = append(total.BlindSpots, next.BlindSpots...)
	total.InvisibleScopes = append(total.InvisibleScopes, next.InvisibleScopes...)
	total.MoreBlindSpots += next.MoreBlindSpots
	total.MoreInvisibleScopes += next.MoreInvisibleScopes
}

func profilePageBounds(
	profileSnapshots []ProfileSnapshot,
	queryHash string,
	sortingVersion string,
	cursorValue string,
	limit int,
	total int,
) (int, int, *string, error) {
	setID := ProfileSetSnapshotID(profileSnapshots)
	offset := 0
	if cursorValue != "" {
		cursor, err := DecodeCursor(cursorValue)
		if err != nil {
			return 0, 0, nil, err
		}
		if err := cursor.ValidateAgainst(setID, queryHash, sortingVersion); err != nil {
			return 0, 0, nil, err
		}
		offset = cursor.Offset
	}
	if offset > total {
		return 0, 0, nil, NewToolError(CodeCursorInvalid, "cursor offset is beyond the merged result")
	}
	end := offset + limit
	if end > total {
		end = total
	}
	var next *string
	if end < total {
		cursor, err := NewCursor(setID, queryHash, end, sortingVersion)
		if err != nil {
			return 0, 0, nil, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return 0, 0, nil, err
		}
		next = &encoded
	}
	return offset, end, next, nil
}

func addOverlapGuidance(
	response *Response[ReferenceResult],
	store *hotsnapshot.SnapshotStore,
	profile string,
) {
	if response == nil || response.Total != 0 || store == nil || store.ProfileCount() < 2 {
		return
	}
	repository := response.Results.Subject.Repository
	if repository == "" {
		return
	}
	selected, err := store.ResolveProfiles([]string{"*"})
	if err != nil {
		return
	}
	overlaps := make([]string, 0)
	for _, candidate := range selected {
		if candidate.Name == profile {
			continue
		}
		snapshot := candidate.Store.Load()
		if snapshot == nil {
			continue
		}
		if _, found := snapshot.RepositoryByName(repository); found {
			overlaps = append(overlaps, candidate.Name)
		}
	}
	if len(overlaps) == 0 {
		return
	}
	if response.Guidance != "" {
		response.Guidance += "; "
	}
	response.Guidance += "repository " + repository + " also exists in profile(s) " +
		strings.Join(overlaps, ", ") + "; this absence is scoped to profile " + profile
}
