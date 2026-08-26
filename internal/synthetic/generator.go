// Package synthetic generates deterministic repository corpora for benchmarks
// and scale tests.
package synthetic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultRepositoryCount = 40
	DefaultFileCount       = 100_000
	DefaultSymbolCount     = 100_000
	DefaultEdgeCount       = 1_000_000
	DefaultSeed            = int64(42)
	defaultOutputDir       = "testdata/generated/synthetic"
)

// Config controls one deterministic synthetic corpus.
type Config struct {
	Repositories int
	Files        int
	Symbols      int
	Edges        int
	Seed         int64
	OutputDir    string
}

// Manifest describes the generated corpus without embedding machine-specific timestamps.
type Manifest struct {
	SchemaVersion       string         `json:"schema_version"`
	Seed                int64          `json:"seed"`
	Repositories        int            `json:"repositories"`
	Files               int            `json:"files"`
	Symbols             int            `json:"symbols"`
	Edges               int            `json:"edges"`
	EdgeCounts          map[string]int `json:"edge_counts"`
	IsolatedSymbolCount int            `json:"isolated_symbol_count"`
	HubSymbols          []string       `json:"hub_symbols"`
	DepthFiveChain      []string       `json:"depth_five_chain"`
	ControlledCycle     []string       `json:"controlled_cycle"`
}

type repositoryRecord struct {
	StableKey string `json:"stable_key"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Language  string `json:"language"`
}

type fileRecord struct {
	StableKey     string `json:"stable_key"`
	RepositoryKey string `json:"repository_key"`
	Path          string `json:"path"`
	ContentHash   string `json:"content_hash"`
	Language      string `json:"language"`
}

type symbolRecord struct {
	StableKey     string `json:"stable_key"`
	RepositoryKey string `json:"repository_key"`
	FileKey       string `json:"file_key"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Signature     string `json:"signature"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
}

type edgeRecord struct {
	Type          string `json:"type"`
	From          string `json:"from"`
	To            string `json:"to"`
	RelationKind  string `json:"relation_kind,omitempty"`
	EvidenceKind  string `json:"evidence_kind,omitempty"`
	SourceFileKey string `json:"source_file_key,omitempty"`
	TargetFileKey string `json:"target_file_key,omitempty"`
}

// DefaultConfig returns the corpus size and output location used by the CLI.
func DefaultConfig() Config {
	return Config{
		Repositories: DefaultRepositoryCount,
		Files:        DefaultFileCount,
		Symbols:      DefaultSymbolCount,
		Edges:        DefaultEdgeCount,
		Seed:         DefaultSeed,
		OutputDir:    defaultOutputDir,
	}
}

// Generate writes a deterministic corpus as JSON Lines plus manifest.json.
func Generate(ctx context.Context, config Config) (Manifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	config = withDefaults(config)
	if err := validate(config); err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(config.OutputDir, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("create output directory: %w", err)
	}

	if err := writeRepositories(ctx, config); err != nil {
		return Manifest{}, err
	}
	if err := writeFiles(ctx, config); err != nil {
		return Manifest{}, err
	}
	if err := writeSymbols(ctx, config); err != nil {
		return Manifest{}, err
	}
	edgeCounts, err := writeEdges(ctx, config)
	if err != nil {
		return Manifest{}, err
	}

	manifest := buildManifest(config, edgeCounts)
	if err := writeJSON(filepath.Join(config.OutputDir, "manifest.json"), manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func withDefaults(config Config) Config {
	defaults := DefaultConfig()
	if config.Repositories == 0 {
		config.Repositories = defaults.Repositories
	}
	if config.Files == 0 {
		config.Files = defaults.Files
	}
	if config.Symbols == 0 {
		config.Symbols = defaults.Symbols
	}
	if config.Edges == 0 {
		config.Edges = defaults.Edges
	}
	if config.OutputDir == "" {
		config.OutputDir = defaults.OutputDir
	}
	return config
}

func validate(config Config) error {
	if config.Repositories < 1 {
		return fmt.Errorf("repositories must be positive: %d", config.Repositories)
	}
	if config.Files < 1 {
		return fmt.Errorf("files must be positive: %d", config.Files)
	}
	if config.Symbols < 9 {
		return fmt.Errorf("symbols must be at least 9 to represent the required graph features: %d", config.Symbols)
	}
	minimumEdges := config.Files + config.Symbols + 8
	if config.Edges < minimumEdges {
		return fmt.Errorf("edges must be at least files + symbols + 8 (%d): %d", minimumEdges, config.Edges)
	}
	return nil
}

func writeRepositories(ctx context.Context, config Config) error {
	return writeJSONLines(filepath.Join(config.OutputDir, "repositories.jsonl"), func(encoder *json.Encoder) error {
		languages := []string{"go", "typescript"}
		for index := range config.Repositories {
			if err := checkContext(ctx, index); err != nil {
				return err
			}
			key := fmt.Sprintf("repository-%04d", index)
			record := repositoryRecord{
				StableKey: key,
				Name:      fmt.Sprintf("repo-%04d", index),
				Path:      fmt.Sprintf("/synthetic/repo-%04d", index),
				Language:  languages[index%len(languages)],
			}
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("write repository %d: %w", index, err)
			}
		}
		return nil
	})
}

func writeFiles(ctx context.Context, config Config) error {
	return writeJSONLines(filepath.Join(config.OutputDir, "files.jsonl"), func(encoder *json.Encoder) error {
		languages := []string{"go", "typescript"}
		for index := range config.Files {
			if err := checkContext(ctx, index); err != nil {
				return err
			}
			repositoryIndex := index % config.Repositories
			record := fileRecord{
				StableKey:     fmt.Sprintf("file-%08d", index),
				RepositoryKey: fmt.Sprintf("repository-%04d", repositoryIndex),
				Path:          fmt.Sprintf("/synthetic/repo-%04d/file-%08d", repositoryIndex, index),
				ContentHash:   fmt.Sprintf("%016x", uint64(index)*0x9e3779b97f4a7c15^uint64(config.Seed)),
				Language:      languages[index%len(languages)],
			}
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("write file %d: %w", index, err)
			}
		}
		return nil
	})
}

func writeSymbols(ctx context.Context, config Config) error {
	return writeJSONLines(filepath.Join(config.OutputDir, "symbols.jsonl"), func(encoder *json.Encoder) error {
		for index := range config.Symbols {
			if err := checkContext(ctx, index); err != nil {
				return err
			}
			fileIndex := index % config.Files
			repositoryIndex := fileIndex % config.Repositories
			name := fmt.Sprintf("symbol_%08d", index)
			record := symbolRecord{
				StableKey:     fmt.Sprintf("symbol-%08d", index),
				RepositoryKey: fmt.Sprintf("repository-%04d", repositoryIndex),
				FileKey:       fmt.Sprintf("file-%08d", fileIndex),
				Name:          name,
				QualifiedName: fmt.Sprintf("repo-%04d.file-%08d.%s", repositoryIndex, fileIndex, name),
				Kind:          []string{"function", "type", "method"}[index%3],
				Signature:     fmt.Sprintf("%s()", name),
				StartLine:     1 + (index % 900),
				EndLine:       1 + (index % 900) + 4,
			}
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("write symbol %d: %w", index, err)
			}
		}
		return nil
	})
}

func writeEdges(ctx context.Context, config Config) (map[string]int, error) {
	edgeCounts := map[string]int{}
	rng := newRNG(config.Seed)
	semanticEdges := config.Edges - config.Files - config.Symbols
	isolatedCount := isolatedSymbolCount(config.Symbols)
	activeSymbols := config.Symbols - isolatedCount
	chainLength := 5
	cycleLength := 3

	err := writeJSONLines(filepath.Join(config.OutputDir, "edges.jsonl"), func(encoder *json.Encoder) error {
		for index := range config.Files {
			if err := checkContext(ctx, index); err != nil {
				return err
			}
			record := edgeRecord{
				Type:         "CONTAINS",
				From:         fmt.Sprintf("repository-%04d", index%config.Repositories),
				To:           fmt.Sprintf("file-%08d", index),
				RelationKind: "repository_file",
			}
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("write contains edge %d: %w", index, err)
			}
			edgeCounts[record.Type]++
		}
		for index := range config.Symbols {
			if err := checkContext(ctx, config.Files+index); err != nil {
				return err
			}
			record := edgeRecord{
				Type:         "DEFINES",
				From:         fmt.Sprintf("file-%08d", index%config.Files),
				To:           fmt.Sprintf("symbol-%08d", index),
				RelationKind: "file_symbol",
			}
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("write defines edge %d: %w", index, err)
			}
			edgeCounts[record.Type]++
		}
		for index := range semanticEdges {
			if err := checkContext(ctx, config.Files+config.Symbols+index); err != nil {
				return err
			}
			record := semanticEdge(index, activeSymbols, chainLength, cycleLength, config.Files, rng)
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("write semantic edge %d: %w", index, err)
			}
			edgeCounts[record.Type]++
		}
		return nil
	})
	return edgeCounts, err
}

func semanticEdge(index, activeSymbols, chainLength, cycleLength, fileCount int, rng *deterministicRNG) edgeRecord {
	if index < chainLength {
		return makeSemanticEdge("REFERENCES", index, index+1, fileCount, "depth_chain")
	}
	index -= chainLength
	if index < cycleLength {
		cycleStart := activeSymbols - cycleLength
		return makeSemanticEdge("CALLS_DIRECT", cycleStart+index, cycleStart+(index+1)%cycleLength, fileCount, "controlled_cycle")
	}
	index -= cycleLength

	var source, target int
	switch index % 4 {
	case 0:
		source, target = 0, rng.intn(activeSymbols)
	case 1:
		source, target = rng.intn(activeSymbols), 0
	case 2:
		source, target = 1, rng.intn(activeSymbols)
	default:
		source, target = rng.intn(activeSymbols), 1
	}
	if source == target {
		target = (target + 1) % activeSymbols
	}
	typeName := "REFERENCES"
	if index%2 == 1 {
		typeName = "CALLS_DIRECT"
	}
	return makeSemanticEdge(typeName, source, target, fileCount, "synthetic_random")
}

func makeSemanticEdge(typeName string, source, target, fileCount int, evidence string) edgeRecord {
	return edgeRecord{
		Type:          typeName,
		From:          fmt.Sprintf("symbol-%08d", source),
		To:            fmt.Sprintf("symbol-%08d", target),
		EvidenceKind:  evidence,
		SourceFileKey: fmt.Sprintf("file-%08d", source%fileCount),
		TargetFileKey: fmt.Sprintf("file-%08d", target%fileCount),
	}
}

func buildManifest(config Config, edgeCounts map[string]int) Manifest {
	isolatedCount := isolatedSymbolCount(config.Symbols)
	activeSymbols := config.Symbols - isolatedCount
	cycleStart := activeSymbols - 3
	chain := make([]string, 0, 6)
	for index := range 6 {
		chain = append(chain, fmt.Sprintf("symbol-%08d", index))
	}
	cycle := make([]string, 0, 3)
	for index := range 3 {
		cycle = append(cycle, fmt.Sprintf("symbol-%08d", cycleStart+index))
	}
	return Manifest{
		SchemaVersion:       "001",
		Seed:                config.Seed,
		Repositories:        config.Repositories,
		Files:               config.Files,
		Symbols:             config.Symbols,
		Edges:               config.Edges,
		EdgeCounts:          edgeCounts,
		IsolatedSymbolCount: isolatedCount,
		HubSymbols:          []string{"symbol-00000000", "symbol-00000001"},
		DepthFiveChain:      chain,
		ControlledCycle:     cycle,
	}
}

func isolatedSymbolCount(symbols int) int {
	count := symbols / 100
	if count < 1 {
		return 1
	}
	return count
}

func checkContext(ctx context.Context, index int) error {
	if index%1024 == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("generate corpus: %w", err)
		}
	}
	return nil
}

func writeJSONLines(path string, write func(*json.Encoder) error) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	encoder := json.NewEncoder(file)
	if err := write(encoder); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

type deterministicRNG struct {
	state uint64
}

func newRNG(seed int64) *deterministicRNG {
	state := uint64(seed)
	if state == 0 {
		state = 0x9e3779b97f4a7c15
	}
	return &deterministicRNG{state: state}
}

func (rng *deterministicRNG) intn(maximum int) int {
	rng.state = rng.state*6364136223846793005 + 1442695040888963407
	return int(rng.state % uint64(maximum))
}
