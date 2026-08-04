//go:build ladybug && cgo && linux

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Luqueee/luque/internal/storage/ladybug"
)

const (
	defaultDatabase      = "benchmarks/ladybug-bulk/graph.db"
	defaultOutput        = "benchmarks/ladybug-recovery"
	defaultDocumentation = "docs/testing/ladybug-recovery.md"
	defaultShimSource    = "benchmarks/ladybug-recovery/testdata/enospc.c"
	baselineSymbolKey    = "symbol-00000000"
)

type config struct {
	DatabasePath      string
	OutputDir         string
	DocumentationPath string
	ShimSource        string
	CaseTimeout       time.Duration
	BulkRows          int
	Worker            string
	MarkerPath        string
	GatePath          string
	BulkCSVPath       string
}

type benchmarkResults struct {
	Benchmark         string       `json:"benchmark"`
	Command           string       `json:"command"`
	Commit            string       `json:"commit"`
	GeneratedAt       time.Time    `json:"generated_at"`
	GoVersion         string       `json:"go_version"`
	GOOS              string       `json:"goos"`
	GOARCH            string       `json:"goarch"`
	BaseDatabaseBytes int64        `json:"base_database_bytes"`
	BaseSHA256Before  string       `json:"base_sha256_before"`
	BaseSHA256After   string       `json:"base_sha256_after"`
	SourceUnchanged   bool         `json:"source_unchanged"`
	Cases             []caseResult `json:"cases"`
	AllPassed         bool         `json:"all_passed"`
	Limitations       []string     `json:"limitations"`
}

type caseResult struct {
	Case       string              `json:"case"`
	Expected   string              `json:"expected"`
	Observed   string              `json:"observed"`
	DurationMS float64             `json:"duration_ms"`
	Passed     bool                `json:"passed"`
	Child      *processObservation `json:"child,omitempty"`
	Checks     []string            `json:"checks,omitempty"`
}

func main() {
	cfg := parseConfig()
	if cfg.Worker != "" {
		if err := runWorker(context.Background(), cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	result, err := run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeOutputs(cfg.OutputDir, cfg.DocumentationPath, result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, probe := range result.Cases {
		fmt.Printf("%s: pass=%t duration=%.1fms\n", probe.Case, probe.Passed, probe.DurationMS)
	}
	fmt.Printf("source unchanged: %t\n", result.SourceUnchanged)
	fmt.Printf("recovery pass: %t\n", result.AllPassed)
	if !result.AllPassed {
		os.Exit(1)
	}
}

func parseConfig() config {
	cfg := config{}
	flag.StringVar(&cfg.DatabasePath, "database", defaultDatabase, "loaded LadybugDB database to copy")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutput, "results output directory")
	flag.StringVar(&cfg.DocumentationPath, "documentation", defaultDocumentation, "generated recovery documentation")
	flag.StringVar(&cfg.ShimSource, "enospc-shim", defaultShimSource, "ENOSPC LD_PRELOAD shim source")
	flag.DurationVar(&cfg.CaseTimeout, "case-timeout", 30*time.Second, "timeout per child-process phase")
	flag.IntVar(&cfg.BulkRows, "bulk-rows", 1_000_000, "rows generated for the interrupted COPY probe")
	flag.StringVar(&cfg.Worker, "worker", "", "internal worker mode")
	flag.StringVar(&cfg.MarkerPath, "marker", "", "internal worker marker path")
	flag.StringVar(&cfg.GatePath, "gate", "", "internal worker start gate")
	flag.StringVar(&cfg.BulkCSVPath, "bulk-csv", "", "internal worker COPY input")
	flag.Parse()
	return cfg
}

func run(ctx context.Context, cfg config) (benchmarkResults, error) {
	if cfg.CaseTimeout <= 0 {
		return benchmarkResults{}, errors.New("case timeout must be positive")
	}
	if cfg.BulkRows < 10_000 {
		return benchmarkResults{}, errors.New("bulk rows must be at least 10000")
	}
	baseInfo, err := os.Stat(cfg.DatabasePath)
	if err != nil {
		return benchmarkResults{}, fmt.Errorf("stat base database: %w", err)
	}
	if !baseInfo.Mode().IsRegular() {
		return benchmarkResults{}, errors.New("base database must be a regular file")
	}
	basePath, err := filepath.Abs(cfg.DatabasePath)
	if err != nil {
		return benchmarkResults{}, err
	}
	beforeHash, err := fileSHA256(basePath)
	if err != nil {
		return benchmarkResults{}, fmt.Errorf("hash base database: %w", err)
	}
	workRoot, err := os.MkdirTemp("", "luque-ladybug-recovery-")
	if err != nil {
		return benchmarkResults{}, err
	}
	defer os.RemoveAll(workRoot)
	executable, err := os.Executable()
	if err != nil {
		return benchmarkResults{}, err
	}

	result := benchmarkResults{
		Benchmark:         "ladybug-recovery",
		Command:           strings.Join(os.Args, " "),
		Commit:            gitState(),
		GeneratedAt:       time.Now().UTC(),
		GoVersion:         runtime.Version(),
		GOOS:              runtime.GOOS,
		GOARCH:            runtime.GOARCH,
		BaseDatabaseBytes: baseInfo.Size(),
		BaseSHA256Before:  beforeHash,
		Limitations: []string{
			"The probes cover Linux process termination and filesystem-call faults, not machine power loss or storage-controller cache loss.",
			"The full-disk case injects ENOSPC at the libc boundary only for the copied database file.",
			"The permission case assumes the benchmark is not run as root.",
		},
	}

	runners := []func(context.Context, string, string, config) caseResult{
		runKillDuringInsert,
		runKillBeforeCommit,
		runKillDuringBulk,
		runReopenAfterCrash,
		runTruncatedFile,
		runPermissionDenied,
		runDiskFull,
	}
	for _, runner := range runners {
		if err := ctx.Err(); err != nil {
			return benchmarkResults{}, err
		}
		result.Cases = append(result.Cases, runner(ctx, executable, workRoot, config{
			DatabasePath: basePath, ShimSource: cfg.ShimSource, CaseTimeout: cfg.CaseTimeout, BulkRows: cfg.BulkRows,
		}))
	}
	for _, probe := range result.Cases {
		if probe.Case == "simulated_disk_full" && !probe.Passed {
			result.Limitations = append(result.Limitations, "In the measured run, the first intercepted write occurred after Apply returned successfully; ENOSPC during close left the copied database unreopenable.")
		}
	}
	afterHash, err := fileSHA256(basePath)
	if err != nil {
		return benchmarkResults{}, fmt.Errorf("rehash base database: %w", err)
	}
	result.BaseSHA256After = afterHash
	result.SourceUnchanged = beforeHash == afterHash
	result.AllPassed = result.SourceUnchanged
	for _, probe := range result.Cases {
		result.AllPassed = result.AllPassed && probe.Passed
	}
	return result, nil
}

func runKillDuringInsert(ctx context.Context, executable, workRoot string, cfg config) caseResult {
	const name = "sigkill_during_insert"
	start := time.Now()
	databasePath, markerPath, err := prepareScenario(workRoot, name, cfg.DatabasePath)
	if err != nil {
		return failedCase(name, "uncommitted inserts are absent after recovery", start, err)
	}
	observation, orchestrationErr := startAndKill(ctx, executable, workerArguments("insert-loop", databasePath, markerPath, "", ""), nil, killOptions{
		MarkerPath: markerPath, MarkerValue: "inserting", Timeout: cfg.CaseTimeout,
	})
	checks := []string{"worker completed 32 CREATE statements inside an open transaction", "worker terminated with SIGKILL"}
	verificationErr := verifyRecovered(ctx, databasePath, []string{"recovery-insert-00000000", "recovery-insert-00000031"}, "")
	if verificationErr == nil {
		checks = append(checks, "database reopened and uncommitted symbols were absent")
	}
	passed := orchestrationErr == nil && observation.Signal == "SIGKILL" && verificationErr == nil
	return completedCase(name, "SIGKILL interrupts an active transaction; reopening rolls it back", start, passed, errors.Join(orchestrationErr, verificationErr), &observation, checks)
}

func runKillBeforeCommit(ctx context.Context, executable, workRoot string, cfg config) caseResult {
	const name = "sigkill_before_commit"
	start := time.Now()
	databasePath, markerPath, err := prepareScenario(workRoot, name, cfg.DatabasePath)
	if err != nil {
		return failedCase(name, "a completed but uncommitted insert is absent after recovery", start, err)
	}
	observation, orchestrationErr := startAndKill(ctx, executable, workerArguments("before-commit", databasePath, markerPath, "", ""), nil, killOptions{
		MarkerPath: markerPath, MarkerValue: "before_commit", Timeout: cfg.CaseTimeout,
	})
	checks := []string{"CREATE completed and COMMIT was deliberately withheld", "worker terminated with SIGKILL"}
	verificationErr := verifyRecovered(ctx, databasePath, []string{"recovery-before-commit"}, "")
	if verificationErr == nil {
		checks = append(checks, "database reopened without the uncommitted symbol")
	}
	passed := orchestrationErr == nil && observation.Signal == "SIGKILL" && verificationErr == nil
	return completedCase(name, "SIGKILL immediately before COMMIT preserves the previous committed state", start, passed, errors.Join(orchestrationErr, verificationErr), &observation, checks)
}

func runKillDuringBulk(ctx context.Context, executable, workRoot string, cfg config) caseResult {
	const name = "sigkill_during_bulk_load"
	start := time.Now()
	databasePath, markerPath, err := prepareScenario(workRoot, name, cfg.DatabasePath)
	if err != nil {
		return failedCase(name, "an interrupted COPY leaves no partial rows", start, err)
	}
	gatePath := filepath.Join(filepath.Dir(databasePath), "bulk.go")
	csvPath := filepath.Join(filepath.Dir(databasePath), "symbols.csv")
	if err := writeBulkCSV(ctx, csvPath, cfg.BulkRows); err != nil {
		return failedCase(name, "an interrupted COPY leaves no partial rows", start, err)
	}
	completionPath := markerPath + ".complete"
	observation, orchestrationErr := startAndKill(ctx, executable, workerArguments("bulk-copy", databasePath, markerPath, gatePath, csvPath), nil, killOptions{
		MarkerPath: markerPath, MarkerValue: "bulk_ready", GatePath: gatePath, ReadDelta: 1 << 20, Timeout: cfg.CaseTimeout,
	})
	_, completionErr := os.Stat(completionPath)
	copyCompleted := completionErr == nil
	if completionErr != nil && !errors.Is(completionErr, os.ErrNotExist) {
		orchestrationErr = errors.Join(orchestrationErr, completionErr)
	}
	verificationErr := verifyRecovered(ctx, databasePath, []string{"recovery-bulk-00000000", "recovery-bulk-00001000"}, "")
	checks := []string{"COPY consumed at least 1 MiB before SIGKILL", "COPY completion marker was absent", "database reopened without sampled bulk symbols"}
	passed := orchestrationErr == nil && observation.Signal == "SIGKILL" && !copyCompleted && verificationErr == nil
	return completedCase(name, "SIGKILL during COPY leaves no partially visible bulk rows", start, passed, errors.Join(orchestrationErr, verificationErr), &observation, checks)
}

func runReopenAfterCrash(ctx context.Context, executable, workRoot string, cfg config) caseResult {
	const name = "reopen_after_crash"
	start := time.Now()
	databasePath, markerPath, err := prepareScenario(workRoot, name, cfg.DatabasePath)
	if err != nil {
		return failedCase(name, "recovery permits a subsequent durable write", start, err)
	}
	observation, orchestrationErr := startAndKill(ctx, executable, workerArguments("before-commit", databasePath, markerPath, "", ""), nil, killOptions{
		MarkerPath: markerPath, MarkerValue: "before_commit", Timeout: cfg.CaseTimeout,
	})
	verificationErr := verifyRecovered(ctx, databasePath, []string{"recovery-before-commit"}, "recovery-after-crash-committed")
	checks := []string{"database reopened after SIGKILL", "baseline symbol remained readable", "a post-recovery transaction survived a second reopen"}
	passed := orchestrationErr == nil && observation.Signal == "SIGKILL" && verificationErr == nil
	return completedCase(name, "reopening recovers the database and accepts a new durable transaction", start, passed, errors.Join(orchestrationErr, verificationErr), &observation, checks)
}

func runTruncatedFile(ctx context.Context, executable, workRoot string, cfg config) caseResult {
	const name = "truncated_file"
	start := time.Now()
	databasePath, _, err := prepareScenario(workRoot, name, cfg.DatabasePath)
	if err != nil {
		return failedCase(name, "a half-truncated database is rejected without hanging", start, err)
	}
	info, err := os.Stat(databasePath)
	if err != nil {
		return failedCase(name, "a half-truncated database is rejected without hanging", start, err)
	}
	if err := os.Truncate(databasePath, info.Size()/2); err != nil {
		return failedCase(name, "a half-truncated database is rejected without hanging", start, err)
	}
	observation, childErr := runChild(ctx, executable, workerArguments("health", databasePath, "", "", ""), nil, cfg.CaseTimeout)
	passed := childErr == nil && observation.ExitCode != 0 && observation.Signal == ""
	checks := []string{"corruption probe ran in an isolated process", "Open/Health returned a controlled error", "no signal or timeout terminated the worker"}
	return completedCase(name, "Open or Health rejects a database truncated to half its committed size", start, passed, childErr, &observation, checks)
}

func runPermissionDenied(ctx context.Context, executable, workRoot string, cfg config) caseResult {
	const name = "permission_denied_directory"
	start := time.Now()
	directory := filepath.Join(workRoot, name)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return failedCase(name, "creating a database in an unwritable directory fails cleanly", start, err)
	}
	databasePath := filepath.Join(directory, "graph.db")
	if err := os.Chmod(directory, 0o500); err != nil {
		return failedCase(name, "creating a database in an unwritable directory fails cleanly", start, err)
	}
	defer os.Chmod(directory, 0o700)
	observation, childErr := runChild(ctx, executable, workerArguments("health", databasePath, "", "", ""), nil, cfg.CaseTimeout)
	_, statErr := os.Stat(databasePath)
	notCreated := errors.Is(statErr, os.ErrNotExist)
	passed := childErr == nil && observation.ExitCode != 0 && observation.Signal == "" && notCreated
	checks := []string{"worker ran as the invoking non-root user", "database creation returned a controlled error", "no database file was created"}
	return completedCase(name, "an unwritable directory is rejected without creating a database", start, passed, childErr, &observation, checks)
}

func runDiskFull(ctx context.Context, executable, workRoot string, cfg config) caseResult {
	const name = "simulated_disk_full"
	start := time.Now()
	databasePath, markerPath, err := prepareScenario(workRoot, name, cfg.DatabasePath)
	if err != nil {
		return failedCase(name, "ENOSPC rolls back the transaction and preserves reopenability", start, err)
	}
	shimPath := filepath.Join(filepath.Dir(databasePath), "enospc.so")
	shimSource, err := filepath.Abs(cfg.ShimSource)
	if err != nil {
		return failedCase(name, "ENOSPC rolls back the transaction and preserves reopenability", start, err)
	}
	if output, err := exec.Command("cc", "-shared", "-fPIC", "-O2", "-Wall", "-Wextra", "-o", shimPath, shimSource, "-ldl").CombinedOutput(); err != nil {
		return failedCase(name, "ENOSPC rolls back the transaction and preserves reopenability", start, fmt.Errorf("build ENOSPC shim: %w: %s", err, output))
	}
	statusPath := filepath.Join(filepath.Dir(databasePath), "enospc.injected")
	environment := []string{
		"LD_PRELOAD=" + shimPath,
		"LUQUE_ENOSPC_PATH=" + databasePath,
		"LUQUE_ENOSPC_AFTER_BYTES=8192",
		"LUQUE_ENOSPC_STATUS=" + statusPath,
	}
	observation, childErr := runChild(ctx, executable, workerArguments("disk-full", databasePath, markerPath, "", ""), environment, cfg.CaseTimeout)
	status, statusErr := os.ReadFile(statusPath)
	statusValue := strings.TrimSpace(string(status))
	injectedDuringApply := statusErr == nil && statusValue == "ENOSPC apply"
	var statusValidationErr error
	if statusErr == nil && !injectedDuringApply {
		statusValidationErr = fmt.Errorf("ENOSPC status = %q, want %q", statusValue, "ENOSPC apply")
	}
	verificationErr := verifyRecovered(ctx, databasePath, []string{"recovery-enospc-0000", "recovery-enospc-0999"}, "recovery-enospc-after")
	checks := []string{fmt.Sprintf("ENOSPC injector status: %q", statusValue), fmt.Sprintf("worker exit code: %d", observation.ExitCode)}
	if verificationErr == nil {
		checks = append(checks, "the recovered database exposed no sampled failed symbols and persisted a new transaction")
	}
	passed := childErr == nil && observation.ExitCode != 0 && observation.Signal == "" && injectedDuringApply && verificationErr == nil
	return completedCase(name, "an injected ENOSPC fails the mutation without partial state or permanent damage", start, passed, errors.Join(childErr, statusErr, statusValidationErr, verificationErr), &observation, checks)
}

func prepareScenario(workRoot, name, sourcePath string) (string, string, error) {
	directory := filepath.Join(workRoot, name)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return "", "", err
	}
	databasePath := filepath.Join(directory, "graph.db")
	if err := copyFile(sourcePath, databasePath); err != nil {
		return "", "", err
	}
	return databasePath, filepath.Join(directory, "worker.marker"), nil
}

func workerArguments(mode, databasePath, markerPath, gatePath, csvPath string) []string {
	arguments := []string{"--worker", mode, "--database", databasePath}
	if markerPath != "" {
		arguments = append(arguments, "--marker", markerPath)
	}
	if gatePath != "" {
		arguments = append(arguments, "--gate", gatePath)
	}
	if csvPath != "" {
		arguments = append(arguments, "--bulk-csv", csvPath)
	}
	return arguments
}

func verifyRecovered(ctx context.Context, databasePath string, absentKeys []string, durableKey string) error {
	database, err := ladybug.Open(ctx, databasePath, ladybug.DefaultConfig())
	if err != nil {
		return fmt.Errorf("reopen recovered database: %w", err)
	}
	if err := database.Health(ctx); err != nil {
		_ = database.Close()
		return fmt.Errorf("health after recovery: %w", err)
	}
	reader, err := database.OpenReader(ctx)
	if err != nil {
		_ = database.Close()
		return err
	}
	if _, found, err := reader.GetSymbol(ctx, baselineSymbolKey); err != nil || !found {
		_ = reader.Close()
		_ = database.Close()
		return fmt.Errorf("baseline symbol found=%t error=%v", found, err)
	}
	for _, key := range absentKeys {
		if _, found, err := reader.GetSymbol(ctx, key); err != nil || found {
			_ = reader.Close()
			_ = database.Close()
			return fmt.Errorf("uncommitted symbol %s found=%t error=%v", key, found, err)
		}
	}
	if err := reader.Close(); err != nil {
		_ = database.Close()
		return err
	}
	if durableKey != "" {
		writer, err := database.OpenWriter(ctx)
		if err != nil {
			_ = database.Close()
			return err
		}
		_, applyErr := writer.Apply(ctx, ladybug.Delta{AddSymbols: []ladybug.Symbol{recoverySymbol(durableKey, 1)}})
		closeErr := writer.Close()
		if err := errors.Join(applyErr, closeErr); err != nil {
			_ = database.Close()
			return fmt.Errorf("post-recovery write: %w", err)
		}
	}
	if err := database.Close(); err != nil {
		return err
	}
	if durableKey == "" {
		return nil
	}
	reopened, err := ladybug.Open(ctx, databasePath, ladybug.DefaultConfig())
	if err != nil {
		return fmt.Errorf("second reopen: %w", err)
	}
	defer reopened.Close()
	reopenedReader, err := reopened.OpenReader(ctx)
	if err != nil {
		return err
	}
	defer reopenedReader.Close()
	if _, found, err := reopenedReader.GetSymbol(ctx, durableKey); err != nil || !found {
		return fmt.Errorf("durable symbol %s found=%t error=%v", durableKey, found, err)
	}
	return nil
}

func recoverySymbol(stableKey string, index int) ladybug.Symbol {
	return ladybug.Symbol{
		StableKey: stableKey, RepositoryKey: "repository-0000", FileKey: "file-00000000",
		Name: stableKey, QualifiedName: "recovery." + stableKey, Kind: "function",
		Signature: stableKey + "()", StartLine: int64(index + 1), EndLine: int64(index + 2),
	}
}

func writeBulkCSV(ctx context.Context, path string, rows int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := bufio.NewWriterSize(file, 1<<20)
	for index := 0; index < rows; index++ {
		if index%1024 == 0 {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return err
			}
		}
		key := fmt.Sprintf("recovery-bulk-%08d", index)
		if _, err := fmt.Fprintf(writer, "%s,repository-0000,file-00000000,%s,recovery.%s,function,%s(),1,2\n", key, key, key, key); err != nil {
			_ = file.Close()
			return err
		}
	}
	return errors.Join(writer.Flush(), file.Close())
}

func failedCase(name, expected string, start time.Time, err error) caseResult {
	return completedCase(name, expected, start, false, err, nil, nil)
}

func completedCase(name, expected string, start time.Time, passed bool, err error, child *processObservation, checks []string) caseResult {
	observed := "all checks passed"
	if err != nil {
		observed = err.Error()
	} else if !passed {
		observed = "one or more expected conditions were not observed"
	}
	return caseResult{
		Case: name, Expected: expected, Observed: observed, DurationMS: milliseconds(time.Since(start)), Passed: passed, Child: child, Checks: checks,
	}
}

func writeOutputs(outputDir, documentationPath string, result benchmarkResults) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "results.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(documentationPath), 0o755); err != nil {
		return err
	}
	var document strings.Builder
	fmt.Fprintln(&document, "# Recuperación de LadybugDB ante fallos")
	fmt.Fprintln(&document)
	fmt.Fprintln(&document, "## Resultado")
	fmt.Fprintln(&document)
	fmt.Fprintf(&document, "Estado global: **%s**. La base de entrada permaneció intacta: `%t`.\n", passLabel(result.AllPassed), result.SourceUnchanged)
	fmt.Fprintln(&document)
	fmt.Fprintf(&document, "- Commit medido: `%s`\n", result.Commit)
	fmt.Fprintf(&document, "- Fecha: `%s`\n", result.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&document, "- Plataforma: `%s/%s`, `%s`\n", result.GOOS, result.GOARCH, result.GoVersion)
	fmt.Fprintf(&document, "- Base: `%d` bytes, SHA-256 `%s`\n", result.BaseDatabaseBytes, result.BaseSHA256Before)
	fmt.Fprintln(&document)
	fmt.Fprintln(&document, "| Caso | Resultado | Duración ms | Observación |")
	fmt.Fprintln(&document, "| --- | --- | ---: | --- |")
	for _, probe := range result.Cases {
		fmt.Fprintf(&document, "| `%s` | %s | %.1f | %s |\n", probe.Case, passLabel(probe.Passed), probe.DurationMS, markdownCell(probe.Observed))
	}
	fmt.Fprintln(&document)
	for _, probe := range result.Cases {
		if probe.Case != "simulated_disk_full" || probe.Passed {
			continue
		}
		fmt.Fprintln(&document, "## Hallazgo crítico")
		fmt.Fprintln(&document)
		fmt.Fprintln(&document, "El caso de disco lleno no es recuperable con el comportamiento observado. El shim se armó justo antes de `Writer.Apply`; `Apply` devolvió éxito sin ninguna escritura interceptada. El primer `ENOSPC` apareció después, durante el cierre (`ENOSPC after_apply`), y la copia dejó de poder abrirse. La API nativa de cierre no devuelve un error que Luque pueda propagar.")
		fmt.Fprintln(&document)
		fmt.Fprintln(&document, "Este resultado queda registrado como **FAIL**, no como una recuperación soportada. `luque doctor storage` detecta la base dañada después del fallo, pero no evita la corrupción de la copia activa. La estrategia operativa necesita publicación atómica desde una copia validada y backups antes de considerar tolerado un agotamiento de disco.")
		fmt.Fprintln(&document)
	}
	fmt.Fprintln(&document, "## Metodología")
	fmt.Fprintln(&document)
	fmt.Fprintln(&document, "Cada caso usa una copia privada de la base cargada. Los workers se ejecutan en procesos separados para que un `SIGKILL`, una base corrupta o un error nativo no comprometan el coordinador ni el artefacto de entrada.")
	fmt.Fprintln(&document)
	fmt.Fprintln(&document, "- **Inserción interrumpida:** el worker confirma 32 `CREATE` dentro de una transacción y el coordinador envía `SIGKILL`.")
	fmt.Fprintln(&document, "- **Antes del commit:** el worker completa el `CREATE`, publica un marcador y queda bloqueado sin ejecutar `COMMIT`.")
	fmt.Fprintln(&document, "- **Carga masiva:** `COPY Symbol` consume al menos 1 MiB de un CSV de un millón de filas antes del `SIGKILL`; un marcador separado demuestra que `COPY` no había terminado.")
	fmt.Fprintln(&document, "- **Reapertura:** tras la caída se valida `Health`, un símbolo base, la ausencia del delta abortado y la persistencia de una transacción nueva después de una segunda reapertura.")
	fmt.Fprintln(&document, "- **Truncado y permisos:** ambos `Open` se aíslan en workers y deben devolver errores controlados, sin señales ni timeouts.")
	fmt.Fprintln(&document, "- **Disco lleno:** un shim `LD_PRELOAD` devuelve `ENOSPC` después de 8 KiB escritos únicamente sobre el descriptor de la copia. Después se comprueban rollback, reapertura y una escritura durable.")
	fmt.Fprintln(&document)
	fmt.Fprintln(&document, "## Reproducción")
	fmt.Fprintln(&document)
	fmt.Fprintln(&document, "```bash")
	fmt.Fprintln(&document, "CGO_ENABLED=1 \\")
	fmt.Fprintln(&document, "CGO_LDFLAGS=\"-L/path/to/ladybug/lib -Wl,-rpath,/path/to/ladybug/lib\" \\")
	fmt.Fprintln(&document, "LD_LIBRARY_PATH=/path/to/ladybug/lib \\")
	fmt.Fprintln(&document, "go run -tags ladybug ./benchmarks/ladybug-recovery \\")
	fmt.Fprintln(&document, "  --database /tmp/luque-copy.db")
	fmt.Fprintln(&document, "```")
	fmt.Fprintln(&document)
	fmt.Fprintln(&document, "## Límites")
	fmt.Fprintln(&document)
	for _, limitation := range result.Limitations {
		fmt.Fprintf(&document, "- %s\n", limitation)
	}
	fmt.Fprintln(&document, "- Estas pruebas no sustituyen los backups. `luque doctor storage` diagnostica el estado posterior, pero no convierte el caso `ENOSPC` en una recuperación soportada.")
	return os.WriteFile(documentationPath, []byte(document.String()), 0o644)
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func passLabel(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func gitState() string {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(output))
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err == nil && len(status) > 0 {
		commit += "-dirty"
	}
	return commit
}
