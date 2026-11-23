package tools

import "strconv"

// The compact spelling of Completeness.
//
// Under `view: "full"` an invisible scope is one row per package, and every row
// spells its repository, its reason and a prose detail. Measured on this
// repository (ADR 0156), thirteen such rows were 5,510 of the 6,089 bytes of a
// `find_references` answer about a name with five references, and the same
// block came back on every call of the session. The repository and the reason
// repeat on nearly every row, and the detail of most of them is a path the
// `fallback` already lists or a compiler message about a fixture.
//
// The compact spelling keeps what a reader decides with: which packages could
// not be read, why, how many failures are behind them, and where to search
// instead. A detail is kept when every row of its group says the same thing,
// which is when it says something about the reason and not about the package;
// otherwise it stays in the full view, and the answer says so.

// scopeGroup is every invisible scope that shares a repository, a reason and a
// file -- the last is empty for a scope, which has no file, and is here so a
// row that names one is never merged into one that does not.
type scopeGroup struct {
	Reason     string `json:"reason"`
	Repository string `json:"repository,omitempty"`
	FilePath   string `json:"file_path,omitempty"`
	// Packages are the requested packages of the scopes, once each, in the
	// order the rows came.
	Packages []string `json:"packages,omitempty"`
	// Occurrences is the number of recorded failures behind every scope of the
	// group: the sum, not a count of rows.
	Occurrences int `json:"occurrences,omitempty"`
	// Detail is present only when every row of the group carried this same
	// text.
	Detail string `json:"detail,omitempty"`
}

// blindSpotGroup is every failed reference that asked for the same thing and
// failed the same way. The places are the evidence and each one stays, as
// `path:line`; what the failures share is spelled once.
type blindSpotGroup struct {
	Reason           string `json:"reason"`
	Repository       string `json:"repository,omitempty"`
	RequestedSymbol  string `json:"requested_symbol,omitempty"`
	RequestedPackage string `json:"requested_package,omitempty"`
	Detail           string `json:"detail,omitempty"`
	// At is the file and line of each failure, in the order the rows came.
	At []string `json:"at,omitempty"`
	// Unlocated counts the failures that carry no file: they are real and
	// there is nothing to point at.
	Unlocated int `json:"unlocated,omitempty"`
}

// compactCompleteness is Completeness as the compact views spell it.
type compactCompleteness struct {
	Verdict        string           `json:"verdict"`
	BlindSpots     []blindSpotGroup `json:"blind_spots,omitempty"`
	MoreBlindSpots int              `json:"more_blind_spots,omitempty"`
	// Repository is the repository every group belongs to, when it is one.
	Repository          string       `json:"repository,omitempty"`
	InvisibleScopes     []scopeGroup `json:"invisible_scopes,omitempty"`
	MoreInvisibleScopes int          `json:"more_invisible_scopes,omitempty"`
	// Details says where the prose a group dropped can be read. It is absent
	// when nothing was dropped.
	Details  string    `json:"details,omitempty"`
	Fallback *Fallback `json:"fallback,omitempty"`
}

// detailsInFullView is what compactCompleteness.Details says.
const detailsInFullView = "view=full"

// compactCompletenessOf spells a verdict for a compact view. It adds nothing
// and invents nothing: every package, every reason and every occurrence of the
// rows is in the result, and the only text it can drop is a detail that the
// rows of a group do not share.
func compactCompletenessOf(source *Completeness) *compactCompleteness {
	if source == nil {
		return nil
	}
	result := &compactCompleteness{
		Verdict:             source.Verdict,
		BlindSpots:          compactBlindSpots(source.BlindSpots),
		MoreBlindSpots:      source.MoreBlindSpots,
		MoreInvisibleScopes: source.MoreInvisibleScopes,
		Fallback:            source.Fallback,
	}
	result.InvisibleScopes, result.Details = compactScopes(source.InvisibleScopes)
	// One repository for the whole verdict, or none: a value that some group
	// lacks would give that group a repository it never had.
	repositories := make([]string, 0, len(result.BlindSpots)+len(result.InvisibleScopes))
	for _, group := range result.BlindSpots {
		repositories = append(repositories, group.Repository)
	}
	for _, group := range result.InvisibleScopes {
		repositories = append(repositories, group.Repository)
	}
	if result.Repository = sharedRepository(repositories); result.Repository != "" {
		for at := range result.BlindSpots {
			result.BlindSpots[at].Repository = ""
		}
		for at := range result.InvisibleScopes {
			result.InvisibleScopes[at].Repository = ""
		}
	}
	return result
}

type blindSpotKey struct{ reason, repository, symbol, pkg, detail string }

func compactBlindSpots(rows []BlindSpot) []blindSpotGroup {
	if len(rows) == 0 {
		return nil
	}
	index := map[blindSpotKey]int{}
	var groups []blindSpotGroup
	for _, row := range rows {
		key := blindSpotKey{row.Reason, row.Repository, row.RequestedSymbol, row.RequestedPackage, row.Detail}
		at, seen := index[key]
		if !seen {
			at = len(groups)
			index[key] = at
			groups = append(groups, blindSpotGroup{
				Reason: row.Reason, Repository: row.Repository, RequestedSymbol: row.RequestedSymbol,
				RequestedPackage: row.RequestedPackage, Detail: row.Detail,
			})
		}
		if row.FilePath == "" && row.StartLine == 0 {
			groups[at].Unlocated++
			continue
		}
		groups[at].At = append(groups[at].At, placeLabel(row.FilePath, row.StartLine))
	}
	return groups
}

// placeLabel is `path:line`, and the path alone when the failure has no line.
func placeLabel(path string, line uint32) string {
	if line == 0 {
		return path
	}
	return path + ":" + strconv.FormatUint(uint64(line), 10)
}

type scopeKey struct{ repository, reason, file string }

func compactScopes(rows []BlindSpot) (groups []scopeGroup, details string) {
	if len(rows) == 0 {
		return nil, ""
	}
	index := map[scopeKey]int{}
	// distinct[i] is the set of details of group i, and rowsWithout[i] says some
	// row of it had none: a text only some rows carry is not the group's.
	var distinct []map[string]struct{}
	var withoutDetail []bool
	for _, row := range rows {
		key := scopeKey{row.Repository, row.Reason, row.FilePath}
		at, seen := index[key]
		if !seen {
			at = len(groups)
			index[key] = at
			groups = append(groups, scopeGroup{Reason: row.Reason, Repository: row.Repository, FilePath: row.FilePath})
			distinct = append(distinct, map[string]struct{}{})
			withoutDetail = append(withoutDetail, false)
		}
		group := &groups[at]
		if row.RequestedPackage != "" && !containsString(group.Packages, row.RequestedPackage) {
			group.Packages = append(group.Packages, row.RequestedPackage)
		}
		group.Occurrences += row.Occurrences
		if row.Detail == "" {
			withoutDetail[at] = true
		} else {
			distinct[at][row.Detail] = struct{}{}
		}
	}
	for at := range groups {
		if len(distinct[at]) == 1 && !withoutDetail[at] {
			for text := range distinct[at] {
				groups[at].Detail = text
			}
			continue
		}
		if len(distinct[at]) > 0 {
			details = detailsInFullView
		}
	}
	return groups, details
}

// sharedRepository is the repository every entry names, and empty when they
// differ, when one has none, or when there are none.
func sharedRepository(repositories []string) string {
	if len(repositories) == 0 {
		return ""
	}
	shared := repositories[0]
	for _, repository := range repositories[1:] {
		if repository != shared {
			return ""
		}
	}
	return shared
}

// compactProfileSnapshot is ProfileSnapshot with its verdict spelled for a
// compact view.
type compactProfileSnapshot struct {
	Name         string               `json:"name"`
	SnapshotID   uint64               `json:"snapshot_id"`
	Completeness *compactCompleteness `json:"completeness,omitempty"`
}

func compactProfiles(profiles []ProfileSnapshot) []compactProfileSnapshot {
	if len(profiles) == 0 {
		return nil
	}
	result := make([]compactProfileSnapshot, len(profiles))
	for at, profile := range profiles {
		result[at] = compactProfileSnapshot{
			Name: profile.Name, SnapshotID: profile.SnapshotID,
			Completeness: compactCompletenessOf(profile.Completeness),
		}
	}
	return result
}
