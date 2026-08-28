package indexing

import "github.com/Luqueee/kivgraph/internal/indexer"

// FullEventKind names one line of the stream `index --full --json` writes to
// stdout.
type FullEventKind string

const (
	// FullEventProgress is one unit of work starting or finishing.
	FullEventProgress FullEventKind = "progress"
	// FullEventResult is what the pass concluded. Exactly one is written, and
	// it is the last line.
	FullEventResult FullEventKind = "result"
)

// FullEvent is one line of that stream.
//
// The stream is the protocol between a server and the index it runs in a child
// process, so it is a declared shape and not the log the command writes for a
// person. A reader ignores a kind it does not know: adding one must not break a
// server built before it existed.
type FullEvent struct {
	Event    FullEventKind    `json:"event"`
	Progress *ProjectProgress `json:"progress,omitempty"`
	Result   *FullDocument    `json:"result,omitempty"`
}

// FullDocument is what one full index pass concluded, in the form that survives
// a process boundary.
//
// It carries what a caller reports and nothing else: the generation that was
// published, the fact counts, and the per-language summary. The diagnostics and
// stage details stay in the child's own report, which it prints for whoever ran
// it; a server forwards its progress and reads this.
type FullDocument struct {
	Passed       bool         `json:"passed"`
	GenerationID string       `json:"generation_id"`
	Counts       Counts       `json:"counts"`
	Index        IndexSummary `json:"index"`
	// Error is why the pass did not pass. It is the child's own message,
	// preserved so the caller reports the reason rather than an exit code.
	Error string `json:"error,omitempty"`
}

// DocumentFromResult projects one in-process result onto the wire form.
func DocumentFromResult(result FullResult) FullDocument {
	return FullDocument{
		Passed:       result.RebuildReport.Passed,
		GenerationID: result.RebuildReport.GenerationID,
		Counts:       result.Counts,
		Index:        SummaryFromReport(result.IndexReport),
	}
}

// SummaryFromReport keeps the phase counts a caller reports and drops the
// detail that belongs to the command that produced them.
//
// Every language is named whether or not it contributed. A language missing
// from the report reads as a language with no code, so the summary must be able
// to say zero.
func SummaryFromReport(report indexer.FullReport) IndexSummary {
	return IndexSummary{
		GoRepositories:              report.GoRepositories,
		GoModules:                   report.GoModules,
		GoDefinitions:               report.GoDefinitions,
		GoReferences:                report.GoReferences,
		GoUnresolved:                report.GoUnresolved,
		TypeScriptRepositories:      report.TypeScriptRepositories,
		TypeScriptSymbols:           report.TypeScriptSymbols,
		TypeScriptReferences:        report.TypeScriptReferences,
		TypeScriptUnresolved:        report.TypeScriptUnresolved,
		RustRepositories:            report.RustRepositories,
		RustWorkspaces:              report.RustWorkspaces,
		RustSymbols:                 report.RustSymbols,
		RustReferences:              report.RustReferences,
		RustUnresolved:              report.RustUnresolved,
		RustWorkspacesNotLoaded:     report.RustWorkspacesNotLoaded,
		GoModulesNotLoaded:          report.GoModulesNotLoaded,
		PythonRepositories:          report.PythonRepositories,
		PythonSymbols:               report.PythonSymbols,
		PythonReferences:            report.PythonReferences,
		PythonUnresolved:            report.PythonUnresolved,
		DartRepositories:            report.DartRepositories,
		DartSymbols:                 report.DartSymbols,
		DartReferences:              report.DartReferences,
		DartUnresolved:              report.DartUnresolved,
		JavaRepositories:            report.JavaRepositories,
		JavaSymbols:                 report.JavaSymbols,
		JavaReferences:              report.JavaReferences,
		JavaUnresolved:              report.JavaUnresolved,
		CSharpRepositories:          report.CSharpRepositories,
		CSharpSymbols:               report.CSharpSymbols,
		CSharpReferences:            report.CSharpReferences,
		CSharpUnresolved:            report.CSharpUnresolved,
		PythonRepositoriesNotLoaded: report.PythonRepositoriesNotLoaded,
		DartRepositoriesNotLoaded:   report.DartRepositoriesNotLoaded,
		JavaRepositoriesNotLoaded:   report.JavaRepositoriesNotLoaded,
		CSharpRepositoriesNotLoaded: report.CSharpRepositoriesNotLoaded,
	}
}

// ProgressFromEvent projects one indexer event onto the project-level step a
// caller reports.
func ProgressFromEvent(event indexer.ProgressEvent) ProjectProgress {
	return ProjectProgress{
		Phase:      string(event.Phase),
		Repository: event.Repository,
		Detail:     event.Detail,
		Completed:  event.Completed,
		Total:      event.Total,
	}
}
