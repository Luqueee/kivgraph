package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/indexing"
)

const (
	startIndexProjectToolName = "start_index_project"
	getIndexStatusToolName    = "get_index_status"
	indexJobHistoryLimit      = 32
)

type GetIndexStatusInput struct {
	OperationID string `json:"operation_id" jsonschema:"Opaque operation_id returned by start_index_project."`
}

type StartIndexProjectResult struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
	PollAfterMS int    `json:"poll_after_ms"`
}

type IndexJobFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type IndexJobStatus struct {
	OperationID string                    `json:"operation_id"`
	Status      string                    `json:"status"`
	StartedAt   string                    `json:"started_at"`
	UpdatedAt   string                    `json:"updated_at"`
	Progress    *indexing.ProjectProgress `json:"progress,omitempty"`
	Result      *indexing.ProjectResult   `json:"result,omitempty"`
	Failure     *IndexJobFailure          `json:"failure,omitempty"`
}

// IndexJobs belongs to one MCP hosting process. Its work deliberately outlives
// the tools/call context that accepted it: otherwise returning an operation ID
// would immediately cancel the operation it names. The process owns the
// goroutine, one operation may run at a time, and completed history is bounded.
type IndexJobs struct {
	mu       sync.RWMutex
	jobs     map[string]IndexJobStatus
	order    []string
	active   bool
	activeID string
	indexer  indexing.ProjectIndexer
}

func NewIndexJobs(indexer indexing.ProjectIndexer) *IndexJobs {
	return &IndexJobs{jobs: make(map[string]IndexJobStatus), indexer: indexer}
}

// RegisterIndexProjectJobs adds the portable asynchronous indexing pair. It
// uses ordinary tools instead of experimental MCP Tasks so every client that
// can call a tool can start an index and poll it without holding one request
// open for the duration of the rebuild.
func RegisterIndexProjectJobs(
	server *sdkmcp.Server,
	jobs *IndexJobs,
	callObservers ...CallObserver,
) {
	if server == nil || jobs == nil || jobs.indexer == nil {
		return
	}
	indexer := jobs.indexer
	callObserver := firstCallObserver(callObservers)
	confirmed := true
	addTextTool(server, &sdkmcp.Tool{
		Name:        startIndexProjectToolName,
		Title:       "Start project index",
		Description: "Starts a consent-gated graph rebuild and returns an operation_id immediately. Poll get_index_status for progress and the result.",
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: &confirmed,
			OpenWorldHint:   &confirmed,
		},
	}, func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments IndexProjectInput,
	) (*sdkmcp.CallToolResult, StartIndexProjectResult, error) {
		started := time.Now()
		batch, err := arguments.projects()
		if err != nil {
			observeCall(nil, callObserver, startIndexProjectToolName, request, started, err)
			return nil, StartIndexProjectResult{}, err
		}
		if err := requireIndexConsent(ctx, request, batch, arguments.Confirmed); err != nil {
			observeCall(nil, callObserver, startIndexProjectToolName, request, started, err)
			return nil, StartIndexProjectResult{}, err
		}
		profile := strings.TrimSpace(arguments.Profile)
		if profile != "" {
			if _, ok := indexer.(profileProjectIndexer); !ok {
				err := NewToolError(CodeInvalidArgument, fmt.Sprintf("project indexer does not support profile %q", profile))
				observeCall(nil, callObserver, startIndexProjectToolName, request, started, err)
				return nil, StartIndexProjectResult{}, err
			}
		}
		operation, err := jobs.start(profile, batch)
		observeCall(nil, callObserver, startIndexProjectToolName, request, started, err)
		if err != nil {
			return nil, StartIndexProjectResult{}, err
		}
		return nil, operation, nil
	})

	addTextTool(server, &sdkmcp.Tool{
		Name:        getIndexStatusToolName,
		Title:       "Get index status",
		Description: "Returns progress, failure, or the published result for an operation_id from start_index_project.",
		Annotations: readOnlyClosedWorld(),
	}, func(
		_ context.Context,
		request *sdkmcp.CallToolRequest,
		arguments GetIndexStatusInput,
	) (*sdkmcp.CallToolResult, IndexJobStatus, error) {
		started := time.Now()
		status, err := jobs.status(arguments.OperationID)
		observeCall(nil, callObserver, getIndexStatusToolName, request, started, err)
		if err != nil {
			return nil, IndexJobStatus{}, err
		}
		return nil, status, nil
	})
}

func (jobs *IndexJobs) start(profile string, batch []indexing.Project) (StartIndexProjectResult, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if jobs.active {
		return StartIndexProjectResult{}, NewToolError(
			CodeIndexingInProgress,
			fmt.Sprintf(
				"index operation %s is already running; poll that operation_id with get_index_status",
				jobs.activeID,
			),
		)
	}
	operationID, err := newIndexOperationID()
	if err != nil {
		// Reaching this branch requires the operating system's randomness
		// source to fail. Injecting a production ID generator solely to force
		// it in a test would violate this repository's test boundary.
		return StartIndexProjectResult{}, WrapToolError(CodeIndexingFailed, "create index operation ID", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	jobs.jobs[operationID] = IndexJobStatus{
		OperationID: operationID,
		Status:      "working",
		StartedAt:   now,
		UpdatedAt:   now,
	}
	jobs.order = append(jobs.order, operationID)
	jobs.active = true
	jobs.activeID = operationID
	go jobs.run(operationID, profile, batch)
	return StartIndexProjectResult{OperationID: operationID, Status: "working", PollAfterMS: 1000}, nil
}

func (jobs *IndexJobs) run(operationID, profile string, batch []indexing.Project) {
	progress := func(update indexing.ProjectProgress) {
		jobs.mu.Lock()
		status := jobs.jobs[operationID]
		status.Progress = &update
		status.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		jobs.jobs[operationID] = status
		jobs.mu.Unlock()
	}
	result, err := runProjectIndex(context.Background(), jobs.indexer, profile, batch, progress)
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	status := jobs.jobs[operationID]
	status.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		status.Status = "failed"
		status.Failure = &IndexJobFailure{
			Code: CodeIndexingFailed, Message: "project indexing failed: " + err.Error(),
		}
	} else {
		status.Status = "completed"
		status.Result = &result
	}
	jobs.jobs[operationID] = status
	jobs.active = false
	jobs.activeID = ""
	jobs.pruneLocked()
}

func (jobs *IndexJobs) status(operationID string) (IndexJobStatus, error) {
	operationID = strings.TrimSpace(operationID)
	if !validIndexOperationID(operationID) {
		return IndexJobStatus{}, NewToolError(
			CodeInvalidArgument,
			"operation_id must be the 32-character lowercase hexadecimal value returned by start_index_project",
		)
	}
	jobs.mu.RLock()
	defer jobs.mu.RUnlock()
	status, ok := jobs.jobs[operationID]
	if !ok {
		return IndexJobStatus{}, NewToolError(
			CodeInvalidArgument,
			"operation_id is unknown or no longer retained; copy it exactly from start_index_project",
		)
	}
	return status, nil
}

func (jobs *IndexJobs) pruneLocked() {
	for len(jobs.order) > indexJobHistoryLimit {
		delete(jobs.jobs, jobs.order[0])
		jobs.order = jobs.order[1:]
	}
}

func newIndexOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func validIndexOperationID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
