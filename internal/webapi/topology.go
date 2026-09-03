package webapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/invalidation"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/topology"
)

func (assembler *topologyAssembler) addRelationship(relationship topologyRelationshipView) bool {
	key := relationshipIdentityKey(relationship)
	if _, exists := assembler.relationshipKeys[key]; exists {
		return true
	}
	if len(assembler.relationships) >= maxTopologyRelationships {
		assembler.markTruncated("relationship limit reached")
		return false
	}
	assembler.relationshipKeys[key] = struct{}{}
	assembler.relationships = append(assembler.relationships, relationship)
	return true
}

func (assembler *topologyAssembler) markTruncated(reason string) {
	assembler.truncated = true
	if assembler.truncatedReason == "" {
		assembler.truncatedReason = reason
	}
}

func newTopologyAssembler() *topologyAssembler {
	return &topologyAssembler{
		repositories:         make(map[topology.LogicalRepositoryID]topology.LogicalRepository),
		declaredRepositories: make(map[topology.LogicalRepositoryID]struct{}),
		worktrees:            make(map[topology.WorktreeID]topology.Worktree),
		profiles:             make([]topologyProfileView, 0),
		sources:              make([]topologySourceView, 0),
		shared:               make(map[topology.WorktreeID]map[string]struct{}),
		relationships:        make([]topologyRelationshipView, 0),
		relationshipKeys:     make(map[string]struct{}),
	}
}

func (assembler *topologyAssembler) addComposition(ctx context.Context, data topologyProfileData) error {
	for _, repository := range data.Composition.Repositories {
		if previous, exists := assembler.repositories[repository.ID]; exists && previous.Name != repository.Name {
			if _, declared := assembler.declaredRepositories[repository.ID]; declared {
				return fmt.Errorf("%w: logical repository %q has conflicting names", errTopologyAmbiguous, repository.ID)
			}
		}
		assembler.repositories[repository.ID] = repository
		assembler.declaredRepositories[repository.ID] = struct{}{}
	}
	for _, worktree := range data.Composition.Worktrees {
		if previous, exists := assembler.worktrees[worktree.ID]; exists &&
			(previous.Repository != worktree.Repository || previous.Path != worktree.Path) {
			return fmt.Errorf("%w: worktree %q has conflicting declarations", errTopologyAmbiguous, worktree.ID)
		}
		assembler.worktrees[worktree.ID] = worktree
	}
	worktreeIDs := make([]string, 0, len(data.Composition.Profile.Worktrees))
	for _, selection := range data.Composition.Profile.Worktrees {
		worktreeIDs = append(worktreeIDs, string(selection.Worktree))
		if assembler.shared[selection.Worktree] == nil {
			assembler.shared[selection.Worktree] = make(map[string]struct{})
		}
		assembler.shared[selection.Worktree][data.Name] = struct{}{}
		assembler.addRelationship(
			structuralRelationship(data.Name, topologyNodeView{Type: "profile", ID: data.Name},
				topologyNodeView{Type: "worktree", ID: string(selection.Worktree)}, "membership"),
		)
	}
	for _, worktree := range data.Composition.Worktrees {
		assembler.addRelationship(
			structuralRelationship(data.Name,
				topologyNodeView{Type: "worktree", ID: string(worktree.ID)},
				topologyNodeView{Type: "repository", ID: string(worktree.Repository)}, "represents"),
		)
	}
	status := "ready"
	reason := ""
	if data.State != nil && data.State.Stale {
		status = "stale"
		reason = data.State.Reason
	} else if !data.ManifestOK {
		status = "partial"
		reason = "indexed source observations are unavailable"
	}
	assembler.profiles = append(assembler.profiles, topologyProfileView{
		ID: data.Name, GenerationID: data.GenerationID, Status: status, Reason: reason, Worktrees: worktreeIDs,
	})
	assembler.addSources(data)
	return assembler.addSnapshotRepositories(ctx, data.Snapshot)
}

func (assembler *topologyAssembler) addSources(data topologyProfileData) {
	byWorktree := make(map[topology.WorktreeID]sourceobservation.Source, len(data.Manifest.Sources))
	if data.ManifestOK {
		for _, source := range data.Manifest.Sources {
			byWorktree[source.Observation.Worktree] = source
		}
	}
	seen := make(map[topology.WorktreeID]struct{}, len(byWorktree))
	for _, worktree := range data.Composition.Worktrees {
		if source, found := byWorktree[worktree.ID]; found {
			status := "unknown"
			reason := "current source state has not been observed"
			var current *topology.SourceObservation
			if data.State != nil {
				status = "current"
				reason = ""
				current = &source.Observation
			}
			assembler.sources = append(assembler.sources, sourceView(data.Name, source.Repository, source.Observation, current, status, reason))
			assembler.applySourceState(&assembler.sources[len(assembler.sources)-1], data.State, worktree.ID)
			seen[worktree.ID] = struct{}{}
			continue
		}
		assembler.sources = append(assembler.sources, topologySourceView{
			Profile: data.Name, Repository: string(worktree.Repository), Worktree: string(worktree.ID),
			Status: "missing", Reason: "worktree has no indexed source observation",
		})
	}
	for worktree, source := range byWorktree {
		if _, found := seen[worktree]; found {
			continue
		}
		status := "unknown"
		reason := "current source state has not been observed"
		var current *topology.SourceObservation
		if data.State != nil {
			status = "current"
			reason = ""
			current = &source.Observation
		}
		assembler.sources = append(assembler.sources, sourceView(data.Name, source.Repository, source.Observation, current, status, reason))
		assembler.applySourceState(&assembler.sources[len(assembler.sources)-1], data.State, worktree)
	}
}

func (assembler *topologyAssembler) applySourceState(view *topologySourceView, state *invalidation.ProfileState, worktree topology.WorktreeID) {
	if state == nil {
		return
	}
	for _, change := range state.Changes {
		if change.Worktree != worktree {
			continue
		}
		view.Reason = change.Detail
		if view.Reason == "" {
			view.Reason = string(change.Reason)
		}
		switch change.Reason {
		case invalidation.ReasonSourceUnavailable:
			view.Status = "unavailable"
			view.Current = nil
		case invalidation.ReasonSourceRemoved:
			view.Status = "missing"
			view.Current = nil
		default:
			view.Status = "stale"
			if change.After != nil {
				view.Current = observationView(*change.After)
			} else {
				view.Current = nil
			}
		}
	}
}

func (assembler *topologyAssembler) addSnapshotRepositories(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot) error {
	metadata := snapshot.Metadata()
	return snapshot.VisitRepositories(ctx, 0, hotsnapshot.RepositoryID(metadata.Counts.Repositories), func(_ hotsnapshot.RepositoryID, record hotsnapshot.RepositoryRecord) error {
		name, ok := snapshot.Strings().String(record.Name)
		if !ok || name == "" {
			return errors.New("snapshot repository has no name")
		}
		id := topology.LogicalRepositoryID(name)
		if _, declared := assembler.declaredRepositories[id]; declared {
			return nil
		}
		if _, exists := assembler.repositories[id]; !exists {
			assembler.repositories[id] = topology.LogicalRepository{ID: id, Name: name}
		}
		return nil
	})
}

// addSnapshotRelationships reuses the expensive relationship projection while
// keeping source status and profile composition request-scoped. The cache has
// one entry per profile, so publication of a new generation replaces the old
// entry instead of retaining every generation seen by a long-lived viewer.
func (handler *Handler) addSnapshotRelationships(
	ctx context.Context,
	assembler *topologyAssembler,
	data topologyProfileData,
) error {
	handler.topologyRelationshipsMu.Lock()
	entry, found := handler.topologyRelationships[data.Name]
	if found {
		handler.touchTopologyRelationshipLocked(data.Name)
	}
	handler.topologyRelationshipsMu.Unlock()
	if !found || entry.GenerationID != data.GenerationID {
		collector := newTopologyAssembler()
		if err := collector.addSnapshotRelationships(ctx, data); err != nil {
			return err
		}
		entry = topologyRelationshipCacheEntry{
			GenerationID: data.GenerationID, Relationships: collector.relationships,
			Truncated: collector.truncated, TruncatedReason: collector.truncatedReason,
		}
		handler.topologyRelationshipsMu.Lock()
		if current, exists := handler.topologyRelationships[data.Name]; exists && current.GenerationID == data.GenerationID {
			entry = current
			handler.touchTopologyRelationshipLocked(data.Name)
		} else {
			handler.topologyRelationships[data.Name] = entry
			handler.touchTopologyRelationshipLocked(data.Name)
			handler.evictTopologyRelationshipsLocked()
		}
		handler.topologyRelationshipsMu.Unlock()
	}
	for _, relationship := range entry.Relationships {
		if !assembler.addRelationship(relationship) {
			break
		}
	}
	if entry.Truncated {
		assembler.markTruncated(entry.TruncatedReason)
	}
	return nil
}

func (handler *Handler) touchTopologyRelationshipLocked(name string) {
	for index, current := range handler.topologyRelationshipLRU {
		if current == name {
			handler.topologyRelationshipLRU = append(handler.topologyRelationshipLRU[:index], handler.topologyRelationshipLRU[index+1:]...)
			break
		}
	}
	handler.topologyRelationshipLRU = append(handler.topologyRelationshipLRU, name)
}

func (handler *Handler) evictTopologyRelationshipsLocked() {
	limit := handler.store.MaxOpenProfiles()
	if limit < 1 {
		limit = 1
	}
	for len(handler.topologyRelationshipLRU) > limit {
		name := handler.topologyRelationshipLRU[0]
		handler.topologyRelationshipLRU = handler.topologyRelationshipLRU[1:]
		delete(handler.topologyRelationships, name)
	}
}

func (assembler *topologyAssembler) addSnapshotRelationships(ctx context.Context, data topologyProfileData) error {
	snapshot := data.Snapshot
	for _, dependency := range snapshot.AllPackageDependencies() {
		source, sourceOK := snapshot.Package(dependency.Source)
		target, targetOK := snapshot.Package(dependency.Target)
		if !sourceOK || !targetOK {
			return errors.New("snapshot package dependency points outside the package table")
		}
		sourceName, err := snapshotRepositoryName(snapshot, source.Repository)
		if err != nil {
			return err
		}
		targetName, err := snapshotRepositoryName(snapshot, target.Repository)
		if err != nil {
			return err
		}
		kind, err := facts.EdgeKindFromCode(dependency.Kind)
		if err != nil {
			return fmt.Errorf("decode package dependency kind: %w", err)
		}
		confidence, provenance, err := decodeRelationshipCodes(dependency.Confidence, dependency.Provenance)
		if err != nil {
			return err
		}
		evidence := ""
		if dependency.Evidence != 0 {
			evidence, err = snapshotString(snapshot, dependency.Evidence)
			if err != nil {
				return err
			}
		}
		if !assembler.addRelationship(topologyRelationshipView{
			Profile: data.Name, Type: "code_dependency",
			Source: topologyNodeView{Type: "repository", ID: sourceName},
			Target: &topologyNodeView{Type: "repository", ID: targetName},
			Kind:   string(kind), Status: relationshipStatus(confidence), Confidence: string(confidence),
			Provenance: string(provenance), Evidence: evidence,
		}) {
			break
		}
	}
	if assembler.truncated {
		return nil
	}
	metadata := snapshot.Metadata()
	if metadata.Counts.Symbols > math.MaxUint32 {
		return errors.New("snapshot symbol count exceeds topology API capacity")
	}
	err := snapshot.VisitSymbols(ctx, 0, hotsnapshot.SymbolID(metadata.Counts.Symbols), func(sourceID hotsnapshot.SymbolID, _ hotsnapshot.SymbolRecord) error {
		sourceRecord, ok := snapshot.Symbol(sourceID)
		if !ok {
			return errors.New("snapshot symbol table is inconsistent")
		}
		sourceFile, ok := snapshot.File(sourceRecord.File)
		if !ok {
			return errors.New("snapshot symbol points outside the file table")
		}
		sourceName, err := snapshotRepositoryName(snapshot, sourceFile.Repository)
		if err != nil {
			return err
		}
		for _, edge := range snapshot.Outgoing(sourceID) {
			targetRecord, ok := snapshot.Symbol(edge.Target)
			if !ok {
				return errors.New("snapshot edge points outside the symbol table")
			}
			targetFile, ok := snapshot.File(targetRecord.File)
			if !ok {
				return errors.New("snapshot edge target points outside the file table")
			}
			targetName, err := snapshotRepositoryName(snapshot, targetFile.Repository)
			if err != nil {
				return err
			}
			kind, err := facts.EdgeKindFromCode(edge.Kind)
			if err != nil {
				return fmt.Errorf("decode symbol edge kind: %w", err)
			}
			confidence, provenance, err := decodeRelationshipCodes(edge.Confidence, edge.Provenance)
			if err != nil {
				return err
			}
			evidence, err := snapshotEvidenceKey(snapshot, edge.Evidence)
			if err != nil {
				return err
			}
			if !assembler.addRelationship(topologyRelationshipView{
				Profile: data.Name, Type: "code_dependency",
				Source: topologyNodeView{Type: "repository", ID: sourceName},
				Target: &topologyNodeView{Type: "repository", ID: targetName},
				Kind:   string(kind), Status: relationshipStatus(confidence), Confidence: string(confidence),
				Provenance: string(provenance), Evidence: evidence,
			}) {
				return errTopologyRelationshipsFull
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errTopologyRelationshipsFull) {
		return fmt.Errorf("iterate snapshot relationships: %w", err)
	}
	if assembler.truncated {
		return nil
	}
	for _, unresolved := range snapshot.UnresolvedReferences() {
		repository, err := snapshotRepositoryName(snapshot, unresolved.Repository)
		if err != nil {
			return err
		}
		reason, err := snapshotString(snapshot, unresolved.Reason)
		if err != nil {
			return err
		}
		detail, err := snapshotString(snapshot, unresolved.Detail)
		if err != nil {
			return err
		}
		status := "unresolved"
		if topologyConflictReason(reason) {
			status = "conflict"
		}
		parts := make([]string, 0, 2)
		for _, part := range []string{reason, detail} {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		if !assembler.addRelationship(topologyRelationshipView{
			Profile: data.Name, Type: "unresolved_reference",
			Source: topologyNodeView{Type: "repository", ID: repository},
			Status: status, Confidence: string(facts.Unresolved), Provenance: "UNRESOLVED_REFERENCE",
			Reason: strings.Join(parts, ": "),
		}) {
			break
		}
	}
	return nil
}

func (assembler *topologyAssembler) response() topologyResponse {
	repositories := make([]topologyRepositoryView, 0, len(assembler.repositories))
	for _, repository := range assembler.repositories {
		repositories = append(repositories, topologyRepositoryView{ID: string(repository.ID), Name: repository.Name})
	}
	sort.Slice(repositories, func(left, right int) bool { return repositories[left].ID < repositories[right].ID })
	worktrees := make([]topologyWorktreeView, 0, len(assembler.worktrees))
	for _, worktree := range assembler.worktrees {
		view := topologyWorktreeView{ID: string(worktree.ID), Repository: string(worktree.Repository), Path: worktree.Path}
		if worktree.Git.GitDirectory != "" || worktree.Git.CommonDirectory != "" {
			view.Git = &topologyGitView{
				GitDirectory: worktree.Git.GitDirectory, CommonDirectory: worktree.Git.CommonDirectory,
			}
		}
		worktrees = append(worktrees, view)
	}
	sort.Slice(worktrees, func(left, right int) bool { return worktrees[left].ID < worktrees[right].ID })
	sort.Slice(assembler.profiles, func(left, right int) bool { return assembler.profiles[left].ID < assembler.profiles[right].ID })
	sort.Slice(assembler.sources, func(left, right int) bool {
		if assembler.sources[left].Profile != assembler.sources[right].Profile {
			return assembler.sources[left].Profile < assembler.sources[right].Profile
		}
		return assembler.sources[left].Worktree < assembler.sources[right].Worktree
	})
	sort.Slice(assembler.relationships, func(left, right int) bool {
		first, second := assembler.relationships[left], assembler.relationships[right]
		return relationshipSortKey(first) < relationshipSortKey(second)
	})
	shared := make([]topologySharedInputView, 0)
	for worktree, owners := range assembler.shared {
		if len(owners) < 2 {
			continue
		}
		values := make([]string, 0, len(owners))
		for owner := range owners {
			values = append(values, owner)
		}
		sort.Strings(values)
		shared = append(shared, topologySharedInputView{Type: "worktree", ID: string(worktree), Owners: values})
	}
	sort.Slice(shared, func(left, right int) bool { return shared[left].ID < shared[right].ID })
	complete := true
	for _, source := range assembler.sources {
		if source.Status == "missing" || source.Status == "unavailable" || source.Status == "unknown" {
			complete = false
			break
		}
	}
	if complete {
		for _, profile := range assembler.profiles {
			if profile.Status == "partial" {
				complete = false
				break
			}
		}
	}
	status := "ready"
	for _, profile := range assembler.profiles {
		if profile.Status == "stale" {
			status = "stale"
			break
		}
		if profile.Status == "partial" {
			status = "partial"
		}
	}
	selected := make([]string, 0, len(assembler.profiles))
	for _, profile := range assembler.profiles {
		selected = append(selected, profile.ID)
	}
	response := topologyResponse{
		APIVersion: APIVersion, TopologyVersion: topology.CurrentSchemaVersion, Status: status,
		SelectedProfiles: selected, Profiles: assembler.profiles, Repositories: repositories,
		Worktrees: worktrees, Sources: assembler.sources, SharedInputs: shared,
		Relationships: assembler.relationships,
		Completeness:  topologyCompletenessView{Complete: complete && !assembler.truncated, Truncated: assembler.truncated},
	}
	reasons := make([]string, 0, 2)
	if !complete {
		reasons = append(reasons, "one or more source observations or indexed manifests are missing or unavailable")
	}
	if assembler.truncated {
		reasons = append(reasons, assembler.truncatedReason)
	}
	response.Completeness.Reason = strings.Join(reasons, "; ")
	if len(assembler.profiles) == 1 {
		response.GenerationID = assembler.profiles[0].GenerationID
	}
	return response
}

func structuralRelationship(profile string, source, target topologyNodeView, kind string) topologyRelationshipView {
	return topologyRelationshipView{
		Profile: profile, Type: kind, Source: source, Target: &target, Status: "structural",
		Confidence: string(facts.StructuralCertain), Provenance: "TOPOLOGY_DECLARATION",
	}
}

func sourceView(profile, repository string, indexed topology.SourceObservation, current *topology.SourceObservation, status, reason string) topologySourceView {
	view := topologySourceView{
		Profile: profile, Repository: repository, Worktree: string(indexed.Worktree), Status: status,
		Reason: reason, Indexed: observationView(indexed),
	}
	if current != nil {
		view.Current = observationView(*current)
	}
	return view
}

func observationView(observation topology.SourceObservation) *topologyObservationView {
	return &topologyObservationView{
		ID: string(observation.ID), Worktree: string(observation.Worktree), Commit: observation.Commit,
		Branch: observation.Branch, Dirty: observation.Dirty, ContentDigest: observation.ContentDigest,
	}
}

func decodeRelationshipCodes(confidenceCode, provenanceCode uint8) (facts.Confidence, facts.Provenance, error) {
	confidence, err := facts.ConfidenceFromCode(confidenceCode)
	if err != nil {
		return "", "", fmt.Errorf("decode relationship confidence: %w", err)
	}
	provenance, err := facts.ProvenanceFromCode(provenanceCode)
	if err != nil {
		return "", "", fmt.Errorf("decode relationship provenance: %w", err)
	}
	return confidence, provenance, nil
}

func relationshipStatus(confidence facts.Confidence) string {
	switch confidence {
	case facts.StructuralCertain:
		return "structural"
	case facts.Candidate:
		return "candidate"
	case facts.Unresolved:
		return "unresolved"
	default:
		if confidence.Exact() {
			return "exact"
		}
		return "conflict"
	}
}

func snapshotRepositoryName(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.RepositoryID) (string, error) {
	record, ok := snapshot.Repository(id)
	if !ok {
		return "", errors.New("snapshot relationship points outside the repository table")
	}
	return snapshotString(snapshot, record.Name)
}

func snapshotString(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.InternedString) (string, error) {
	value, ok := snapshot.Strings().String(id)
	if !ok {
		return "", errors.New("snapshot relationship points outside the string table")
	}
	return value, nil
}

func snapshotEvidenceKey(snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.EvidenceID) (string, error) {
	if id == hotsnapshot.InvalidEvidenceID {
		return "", nil
	}
	record, ok := snapshot.Evidence(id)
	if !ok {
		return "", errors.New("snapshot edge points outside the evidence table")
	}
	return snapshotString(snapshot, record.Key)
}

func relationshipSortKey(relationship topologyRelationshipView) string {
	target := ""
	if relationship.Target != nil {
		target = relationship.Target.Type + ":" + relationship.Target.ID
	}
	return strings.Join([]string{
		relationship.Profile, relationship.Type, relationship.Source.Type + ":" + relationship.Source.ID,
		target, relationship.Kind, relationship.Status, relationship.Evidence,
	}, "\x00")
}

func relationshipIdentityKey(relationship topologyRelationshipView) string {
	return strings.Join([]string{
		relationshipSortKey(relationship), relationship.Confidence, relationship.Provenance, relationship.Reason,
	}, "\x00")
}

func topologyConflictReason(reason string) bool {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "AMBIGUOUS_MODULE_PROVIDER", "AMBIGUOUS_PACKAGE_PROVIDER", "AMBIGUOUS_SYMBOL",
		"CONFLICT", "CONFLICTING_DEFINITIONS", "REPLACE_CONFLICT":
		return true
	default:
		return false
	}
}

func parseTopologyQuery(values url.Values) (topologyQuery, error) {
	query := topologyQuery{GenerationPins: make(map[string]string)}
	query.Profiles = append(query.Profiles, values["profile"]...)
	for _, profile := range query.Profiles {
		if profile != "*" {
			if _, err := topology.NewProfileID(profile); err != nil {
				return topologyQuery{}, fmt.Errorf("profile: %w", err)
			}
		}
	}
	generationIDs := values["generation_id"]
	if len(generationIDs) > 1 {
		return topologyQuery{}, errors.New("generation_id may be supplied only once")
	}
	if len(generationIDs) == 1 {
		if _, err := topology.NewGenerationID(generationIDs[0]); err != nil {
			return topologyQuery{}, fmt.Errorf("generation_id: %w", err)
		}
		query.GenerationID = generationIDs[0]
	}
	for _, pin := range values["generation"] {
		profile, generationID, found := strings.Cut(pin, ":")
		if !found || profile == "" || generationID == "" || strings.Contains(generationID, ":") {
			return topologyQuery{}, errors.New("generation must use profile:generation format")
		}
		if _, err := topology.NewProfileID(profile); err != nil {
			return topologyQuery{}, fmt.Errorf("generation profile: %w", err)
		}
		if _, err := topology.NewGenerationID(generationID); err != nil {
			return topologyQuery{}, fmt.Errorf("generation pin: %w", err)
		}
		if _, exists := query.GenerationPins[profile]; exists {
			return topologyQuery{}, fmt.Errorf("generation pin for profile %q was supplied more than once", profile)
		}
		query.GenerationPins[profile] = generationID
	}
	return query, nil
}

func (handler *Handler) topology(writer http.ResponseWriter, request *http.Request) {
	query, err := parseTopologyQuery(request.URL.Query())
	if err != nil {
		handler.writeError(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	response, err := handler.buildTopology(request.Context(), query)
	if err != nil {
		status, code := http.StatusInternalServerError, "INTERNAL"
		switch {
		case errors.Is(err, errTopologyInvalidArgument):
			status, code = http.StatusBadRequest, "INVALID_ARGUMENT"
		case errors.Is(err, errTopologyGenerationChange):
			status, code = http.StatusConflict, "GENERATION_CHANGED"
		case errors.Is(err, errTopologyUnavailable):
			status, code = http.StatusServiceUnavailable, "TOPOLOGY_UNAVAILABLE"
		case errors.Is(err, errTopologyAmbiguous):
			status, code = http.StatusConflict, "TOPOLOGY_AMBIGUOUS"
		case errors.Is(err, hotsnapshot.ErrSnapshotStoreClosed):
			status, code = http.StatusServiceUnavailable, "TOPOLOGY_UNAVAILABLE"
		}
		handler.logger.Error("topology request failed", "code", code, "error", err)
		handler.writeError(writer, request, status, code, topologyClientError(code, err))
		return
	}
	handler.writeJSON(writer, request, http.StatusOK, response)
}

func (handler *Handler) buildTopology(ctx context.Context, query topologyQuery) (topologyResponse, error) {
	selections, err := handler.resolveTopologyStores(query.Profiles)
	if err != nil {
		return topologyResponse{}, err
	}
	if len(selections) == 0 {
		return topologyResponse{}, fmt.Errorf("%w: no profiles selected", errTopologyUnavailable)
	}
	if err := validateTopologyPins(selections, query); err != nil {
		return topologyResponse{}, err
	}
	if query.GenerationID != "" && (len(selections) > 1 || len(query.GenerationPins) > 0) {
		return topologyResponse{}, fmt.Errorf("%w: generation_id cannot be combined with multiple profile pins", errTopologyInvalidArgument)
	}
	if handler.topologyOptions.ConfigPath == "" {
		for _, selection := range selections {
			if _, known := selection.Store.ActiveID(); !known {
				return topologyResponse{}, fmt.Errorf("%w: profile %q: no published generation", errTopologyUnavailable, selection.Name)
			}
		}
		return topologyResponse{}, fmt.Errorf("%w: configuration path is required", errTopologyUnavailable)
	}
	data := make([]topologyProfileData, 0, len(selections))
	for _, selection := range selections {
		profileData, err := handler.loadTopologyProfile(ctx, selection, query)
		if err != nil {
			return topologyResponse{}, err
		}
		data = append(data, profileData)
	}
	assembler := newTopologyAssembler()
	for _, profileData := range data {
		if err := assembler.addComposition(ctx, profileData); err != nil {
			return topologyResponse{}, err
		}
		if err := handler.addSnapshotRelationships(ctx, assembler, profileData); err != nil {
			return topologyResponse{}, fmt.Errorf("profile %q: %w", profileData.Name, err)
		}
	}
	for index, selection := range selections {
		current, known := selection.Store.ActiveID()
		if !known || current != data[index].Generation {
			return topologyResponse{}, fmt.Errorf("%w: profile %q changed from generation %s", errTopologyGenerationChange, selection.Name, data[index].GenerationID)
		}
	}
	return assembler.response(), nil
}

func (handler *Handler) resolveTopologyStores(requested []string) ([]topologyStoreSelection, error) {
	if handler.store == nil {
		return nil, fmt.Errorf("%w: no published generation", errTopologyUnavailable)
	}
	resolved, err := handler.store.ResolveProfiles(requested)
	if err != nil && handler.store.ProfileCount() == 1 && len(requested) == 1 && requested[0] == handler.topologyOptions.Profile {
		return []topologyStoreSelection{{Name: handler.topologyOptions.Profile, Store: handler.store}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: profile selection: %v", errTopologyInvalidArgument, err)
	}
	defaultName := handler.topologyOptions.Profile
	if defaultName == "" {
		loaded, loadErr := config.LoadProfile(handler.topologyOptions.ConfigPath, "")
		if loadErr != nil {
			return nil, fmt.Errorf("%w: load default profile: %v", errTopologyUnavailable, loadErr)
		}
		defaultName = loaded.Profile
	}
	selections := make([]topologyStoreSelection, 0, len(resolved))
	for _, profile := range resolved {
		name := profile.Name
		if name == "" {
			name = defaultName
		}
		selections = append(selections, topologyStoreSelection{Name: name, Store: profile.Store})
	}
	return selections, nil
}

func validateTopologyPins(selections []topologyStoreSelection, query topologyQuery) error {
	if len(query.GenerationPins) == 0 {
		return nil
	}
	selected := make(map[string]struct{}, len(selections))
	for _, selection := range selections {
		selected[selection.Name] = struct{}{}
		if _, found := query.GenerationPins[selection.Name]; !found {
			return fmt.Errorf("%w: every selected profile needs a generation pin", errTopologyInvalidArgument)
		}
	}
	for profile := range query.GenerationPins {
		if _, found := selected[profile]; !found {
			return fmt.Errorf("%w: generation pin is for unselected profile %q", errTopologyInvalidArgument, profile)
		}
	}
	return nil
}

func (handler *Handler) loadTopologyProfile(ctx context.Context, selection topologyStoreSelection, query topologyQuery) (topologyProfileData, error) {
	numeric, known := selection.Store.ActiveID()
	if !known {
		reason := "no published generation"
		if failure := selection.Store.LoadFailure(); failure != nil {
			reason = failure.Error()
		}
		return topologyProfileData{}, fmt.Errorf("%w: profile %q: %s", errTopologyUnavailable, selection.Name, reason)
	}
	generationID := fmt.Sprintf("%06d", numeric)
	if _, err := topology.NewGenerationID(generationID); err != nil {
		return topologyProfileData{}, fmt.Errorf("%w: profile %q has invalid generation %q: %v", errTopologyUnavailable, selection.Name, generationID, err)
	}
	if query.GenerationID != "" {
		if query.GenerationID != generationID {
			return topologyProfileData{}, fmt.Errorf("%w: profile %q is at generation %s; refresh the topology", errTopologyGenerationChange, selection.Name, generationID)
		}
	}
	if expected, pinned := query.GenerationPins[selection.Name]; pinned && expected != generationID {
		return topologyProfileData{}, fmt.Errorf("%w: profile %q is at generation %s; refresh the topology", errTopologyGenerationChange, selection.Name, generationID)
	}
	snapshot := selection.Store.Load()
	if snapshot == nil {
		reason := "published generation could not be loaded"
		if failure := selection.Store.LoadFailure(); failure != nil {
			reason = failure.Error()
		}
		return topologyProfileData{}, fmt.Errorf("%w: profile %q generation %s: %s", errTopologyUnavailable, selection.Name, generationID, reason)
	}
	if snapshot.Metadata().ID != numeric {
		return topologyProfileData{}, fmt.Errorf("%w: profile %q changed while loading generation %s; refresh the topology", errTopologyGenerationChange, selection.Name, generationID)
	}
	loaded, err := config.LoadProfile(handler.topologyOptions.ConfigPath, selection.Name)
	if err != nil {
		return topologyProfileData{}, fmt.Errorf("%w: load profile %q: %v", errTopologyUnavailable, selection.Name, err)
	}
	value, present, err := config.LoadProfileTopology(handler.topologyOptions.ConfigPath, selection.Name)
	if err != nil {
		return topologyProfileData{}, fmt.Errorf("%w: load profile %q topology: %v", errTopologyUnavailable, selection.Name, err)
	}
	manifest, manifestOK, state, stateLoaded, err := handler.profileManifest(ctx, loaded, generationID)
	if err != nil {
		return topologyProfileData{}, err
	}
	if !stateLoaded {
		state, err = handler.profileInvalidationState(ctx, loaded, selection.Name)
		if err != nil {
			return topologyProfileData{}, err
		}
	}
	var profileState *invalidation.ProfileState
	for index := range state.Profiles {
		if state.Profiles[index].Profile == selection.Name && state.Profiles[index].Generation == generationID {
			copy := state.Profiles[index]
			profileState = &copy
			break
		}
	}
	var composition topology.ProfileComposition
	if present {
		composition, err = value.Compose(topology.ProfileID(selection.Name))
	} else {
		composition, err = legacyComposition(loaded, manifest)
	}
	if err != nil {
		return topologyProfileData{}, fmt.Errorf("%w: compose profile %q: %v", errTopologyAmbiguous, selection.Name, err)
	}
	return topologyProfileData{
		Name: selection.Name, GenerationID: generationID, Generation: numeric, Snapshot: snapshot,
		Composition: composition, Manifest: manifest, ManifestOK: manifestOK, State: profileState,
	}, nil
}

func (handler *Handler) profileManifest(
	ctx context.Context,
	loaded config.Loaded,
	generationID string,
) (sourceobservation.Manifest, bool, invalidation.State, bool, error) {
	root := filepath.Dir(loaded.Config.Storage.DatabasePath)
	path := filepath.Join(generation.GenerationsDir(root), generationID)
	manifest, err := sourceobservation.Read(path)
	if err == nil {
		return manifest, true, invalidation.State{}, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sourceobservation.Manifest{}, false, invalidation.State{}, false,
			fmt.Errorf("%w: read source observations for generation %s: %v", errTopologyUnavailable, generationID, err)
	}
	state, stateErr := handler.profileInvalidationState(ctx, loaded, loaded.Profile)
	if stateErr != nil {
		return sourceobservation.Manifest{}, false, invalidation.State{}, false, stateErr
	}
	for _, profile := range state.Profiles {
		if profile.Profile == loaded.Profile && profile.Generation == generationID {
			return profile.Manifest, true, state, true, nil
		}
	}
	return sourceobservation.Manifest{}, false, state, true, nil
}

func topologyClientError(code string, err error) string {
	switch code {
	case "INVALID_ARGUMENT":
		return err.Error()
	case "GENERATION_CHANGED":
		return "selected topology generation changed; refresh the topology"
	case "TOPOLOGY_AMBIGUOUS":
		return "topology declarations are ambiguous; resolve them before retrying"
	case "TOPOLOGY_UNAVAILABLE":
		if strings.Contains(err.Error(), "no published generation") {
			return "no published generation"
		}
		return "topology is unavailable; retry after a generation is published"
	default:
		return "topology request failed"
	}
}

func (handler *Handler) profileInvalidationState(ctx context.Context, loaded config.Loaded, profile string) (invalidation.State, error) {
	root := handler.topologyOptions.InvalidationRoot
	if root == "" {
		databaseDirectory := filepath.Dir(loaded.Config.Storage.DatabasePath)
		if filepath.Base(filepath.Dir(databaseDirectory)) == "profiles" {
			root = filepath.Dir(filepath.Dir(databaseDirectory))
		} else {
			root = databaseDirectory
		}
	}
	manager, err := invalidation.Open(root)
	if err != nil {
		return invalidation.State{}, fmt.Errorf("%w: open invalidation state for profile %q: %v", errTopologyUnavailable, profile, err)
	}
	if err := manager.Refresh(ctx); err != nil {
		return invalidation.State{}, fmt.Errorf("%w: refresh invalidation state for profile %q: %v", errTopologyUnavailable, profile, err)
	}
	return manager.Snapshot(), nil
}

func legacyComposition(loaded config.Loaded, manifest sourceobservation.Manifest) (topology.ProfileComposition, error) {
	observed := make(map[string]topology.WorktreeID, len(manifest.Sources))
	for _, source := range manifest.Sources {
		observed[source.Repository] = source.Observation.Worktree
	}
	composition := topology.ProfileComposition{Profile: topology.Profile{ID: topology.ProfileID(loaded.Profile)}}
	for _, configured := range loaded.Repositories.Repositories {
		repositoryID, err := topology.NewLogicalRepositoryID(configured.Name)
		if err != nil {
			return topology.ProfileComposition{}, fmt.Errorf("legacy repository %q: %w", configured.Name, err)
		}
		worktreeID := observed[configured.Name]
		if worktreeID == "" {
			worktreeID, err = topology.NewWorktreeID("legacy:" + configured.Name)
			if err != nil {
				return topology.ProfileComposition{}, fmt.Errorf("legacy worktree %q: %w", configured.Name, err)
			}
		}
		composition.Repositories = append(composition.Repositories, topology.LogicalRepository{ID: repositoryID, Name: configured.Name})
		composition.Worktrees = append(composition.Worktrees, topology.Worktree{ID: worktreeID, Repository: repositoryID, Path: configured.Path})
		composition.Profile.Worktrees = append(composition.Profile.Worktrees, topology.WorktreeSelection{Repository: repositoryID, Worktree: worktreeID})
	}
	return composition, nil
}
