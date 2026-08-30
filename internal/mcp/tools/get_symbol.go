package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

const getSymbolToolName = "get_symbol"

// GetSymbolInput identifies one symbol, either by its durable stable key or by
// the repository, path and qualified name every row of this surface carries.
type GetSymbolInput struct {
	Profile        []string `json:"profile,omitempty" jsonschema:"Profiles to query; omit for the default, or use * alone for all."`
	StableKey      string   `json:"stable_key,omitempty" jsonschema:"The symbol durable key, as a detailed result returns it. The triple below works instead."`
	QualifiedName  string   `json:"qualified_name,omitempty" jsonschema:"The symbol fully qualified name, as every row of this surface carries it."`
	Repository     string   `json:"repository,omitempty" jsonschema:"The repository that declares the symbol. Pass it with qualified_name and path."`
	Path           string   `json:"path,omitempty" jsonschema:"The repository-relative file that declares the symbol."`
	ResponseFormat string   `json:"response_format,omitempty" jsonschema:"concise (the default) omits the derived identifiers; detailed returns them."`
}

// SymbolDetails is the public detail shape returned for one symbol. The
// derived identifiers -- the canonical identity and the repository key, whose
// value is already spelled out by the name and the path beside it -- are
// returned only for the detailed format.
type SymbolDetails struct {
	Profiles       ProfileNames `json:"profile,omitempty"`
	StableKey      string       `json:"stable_key"`
	Repository     string       `json:"repository"`
	RepositoryPath string       `json:"repository_path"`
	PackageName    string       `json:"package_name"`
	ModulePath     string       `json:"module_path"`
	FilePath       string       `json:"file_path"`
	Name           string       `json:"name"`
	QualifiedName  string       `json:"qualified_name"`
	Kind           string       `json:"kind"`
	Signature      string       `json:"signature"`
	Exported       bool         `json:"exported"`
	StartLine      uint32       `json:"start_line"`
	EndLine        uint32       `json:"end_line"`

	CanonicalIdentity string          `json:"canonical_identity,omitempty"`
	RepositoryKey     string          `json:"repository_key,omitempty"`
	Variants          []SymbolDetails `json:"-"`
}

// MarshalJSON preserves the historical object for one profile and emits the
// independently observed variants as rows for a multi-profile union.
func (details SymbolDetails) MarshalJSON() ([]byte, error) {
	if details.Variants != nil {
		return json.Marshal(details.Variants)
	}
	type wire SymbolDetails
	return json.Marshal(wire(details))
}

// RegisterGetSymbol adds the read-only symbol lookup tool without a graph
// source. Calls require a snapshot-backed registration to return data.
func RegisterGetSymbol(server *sdkmcp.Server) {
	RegisterGetSymbolWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterGetSymbolWithObserver adds get_symbol and optionally observes
// handler latency.
func RegisterGetSymbolWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterGetSymbolWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterGetSymbolWithSnapshotStore registers get_symbol over the immutable
// snapshot currently published by snapshotStore.
func RegisterGetSymbolWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterGetSymbolWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterGetSymbolWithObserverAndSnapshotStore registers get_symbol over a
// snapshot store and optionally observes latency.
func RegisterGetSymbolWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetSymbolInput,
	) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
		if snapshotStore != nil {
			if profileErr := RequireStableKeyProfile(snapshotStore.ProfileCount(), arguments.StableKey, arguments.Profile); profileErr != nil {
				return nil, Response[SymbolDetails]{}, profileErr
			}
			selected, selectionErr := snapshotStore.ResolveProfiles(arguments.Profile)
			if selectionErr != nil {
				return nil, Response[SymbolDetails]{}, WrapToolError(CodeInvalidArgument, selectionErr.Error(), selectionErr)
			}
			if len(selected) > 1 {
				return getSymbolAcrossProfiles(ctx, request, arguments, selected)
			}
		}
		store, profile, count, err := resolveSingleProfile(snapshotStore, arguments.Profile, arguments.StableKey)
		if err != nil {
			return nil, Response[SymbolDetails]{}, err
		}
		result, response, err := getSymbol(ctx, request, arguments, store)
		scopeResponse(&response, profile, count)
		return result, response, err
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GetSymbolInput,
		) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
			start := time.Now()
			result, symbol, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, getSymbolToolName, start, symbol, err)
			return result, symbol, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        getSymbolToolName,
		Description: "One symbol's package, signature, visibility and line range, by stable key or by repository, path and qualified name.",
		Annotations: readOnlyClosedWorld(),
	}, handler)
}

func getSymbolAcrossProfiles(
	ctx context.Context,
	request *sdkmcp.CallToolRequest,
	arguments GetSymbolInput,
	selected []hotsnapshot.ProfileStore,
) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
	profiles := make([]ProfileSnapshot, 0, len(selected))
	rows := make([]SymbolDetails, 0, len(selected))
	variants := make(map[string]int)
	for _, profile := range selected {
		snapshot := profile.Store.Load()
		if snapshot == nil {
			return nil, Response[SymbolDetails]{}, ErrIndexNotReady()
		}
		profiles = append(profiles, ProfileSnapshot{Name: profile.Name, SnapshotID: snapshot.Metadata().ID})
		profileArguments := arguments
		profileArguments.Profile = nil
		_, response, queryErr := getSymbol(ctx, request, profileArguments, profile.Store)
		if queryErr != nil {
			if code := ErrorCode(queryErr); code == CodeSymbolNotFound || code == CodeRepositoryNotFound {
				continue
			}
			return nil, Response[SymbolDetails]{}, queryErr
		}
		row := response.Results
		row.Profiles = ""
		payload, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return nil, Response[SymbolDetails]{}, WrapToolError(CodeSnapshotUnavailable, "encode symbol details for profile merge", marshalErr)
		}
		key := row.StableKey + "\x00" + string(payload)
		if position, found := variants[key]; found {
			rows[position].Profiles = rows[position].Profiles.append(profile.Name)
			continue
		}
		row.Profiles = profileNames(profile.Name)
		variants[key] = len(rows)
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, Response[SymbolDetails]{}, NewToolError(CodeSymbolNotFound, "symbol was not found in the selected profiles")
	}
	return nil, Response[SymbolDetails]{
		Profiles: profiles, CrossProfileEdges: "not_resolved",
		Total: len(rows), Returned: len(rows),
		Results: SymbolDetails{Variants: rows},
	}, nil
}

func getSymbol(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments GetSymbolInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[SymbolDetails], error) {
	selector, err := normalizeSymbolSelector(arguments.StableKey, arguments.Repository, arguments.Path, arguments.QualifiedName)
	if err != nil {
		return nil, Response[SymbolDetails]{}, err
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return nil, Response[SymbolDetails]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[SymbolDetails]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[SymbolDetails]{}, ErrIndexNotReady()
	}

	symbolID, err := resolveSymbolSelector(snapshot, selector)
	if err != nil {
		return nil, Response[SymbolDetails]{}, err
	}
	symbol, found := snapshot.Symbol(symbolID)
	if !found {
		return nil, Response[SymbolDetails]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot symbol index is inconsistent",
			fmt.Errorf("symbol index %d is missing", symbolID),
		)
	}
	details, err := symbolDetails(snapshot, symbol, format)
	if err != nil {
		return nil, Response[SymbolDetails]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot contains invalid symbol metadata",
			err,
		)
	}

	metadata := snapshot.Metadata()
	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[SymbolDetails]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &snapshotAgeMS,
		Total:         1,
		Returned:      1,
		Results:       details,
	}, nil
}

func symbolDetails(
	snapshot *hotsnapshot.GraphSnapshot,
	symbol hotsnapshot.SymbolRecord,
	format string,
) (SymbolDetails, error) {
	summary, err := symbolSummary(snapshot, symbol, format)
	if err != nil {
		return SymbolDetails{}, err
	}
	location, err := resolveSymbolLocation(snapshot, symbol)
	if err != nil {
		return SymbolDetails{}, err
	}
	details := SymbolDetails{
		StableKey:      summary.StableKey,
		Repository:     location.RepositoryName,
		RepositoryPath: location.RepositoryPath,
		PackageName:    location.PackageName,
		ModulePath:     location.ModulePath,
		FilePath:       location.FilePath,
		Name:           summary.Name,
		QualifiedName:  summary.QualifiedName,
		Kind:           summary.Kind,
		Signature:      summary.Signature,
		Exported:       summary.Exported,
		StartLine:      symbol.StartLine,
		EndLine:        symbol.EndLine,
	}
	if format == ResponseFormatDetailed {
		details.CanonicalIdentity = summary.CanonicalIdentity
		details.RepositoryKey = location.RepositoryKey
	}
	return details, nil
}
