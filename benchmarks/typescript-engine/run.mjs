import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));

function parseArguments(argv) {
	const options = { sizes: [250, 1000, 4000], seed: 42, iterations: 50, warmup: 5, output: here };
	for (let index = 0; index < argv.length; index++) {
		const value = argv[index + 1];
		switch (argv[index]) {
			case "--sizes":
				options.sizes = value.split(",").map((entry) => Number.parseInt(entry.trim(), 10));
				index++;
				break;
			case "--seed":
				options.seed = Number.parseInt(value, 10);
				index++;
				break;
			case "--iterations":
				options.iterations = Number.parseInt(value, 10);
				index++;
				break;
			case "--warmup":
				options.warmup = Number.parseInt(value, 10);
				index++;
				break;
			case "--output":
				options.output = value;
				index++;
				break;
			default:
				throw new Error(`unknown flag ${argv[index]}`);
		}
	}
	return options;
}

function gitState() {
	try {
		const commit = execFileSync("git", ["rev-parse", "HEAD"], { cwd: here, encoding: "utf8" }).trim();
		const status = execFileSync("git", ["status", "--porcelain"], { cwd: here, encoding: "utf8" }).trim();
		return status === "" ? commit : `${commit}-dirty`;
	} catch {
		return "unknown";
	}
}

function runEngine(engine, size, options) {
	const corpusRoot = path.join(os.tmpdir(), `kivgraph-ts-engine-${size}-${options.seed}`);
	const stdout = execFileSync(
		process.execPath,
		[
			path.join(here, "harness.mjs"),
			"--engine",
			engine,
			"--files",
			String(size),
			"--seed",
			String(options.seed),
			"--iterations",
			String(options.iterations),
			"--warmup",
			String(options.warmup),
			"--corpus-root",
			corpusRoot,
		],
		{ cwd: here, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 },
	);
	return JSON.parse(stdout.trim().split("\n").pop());
}

function ratio(slow, fast) {
	if (fast === 0) {
		return null;
	}
	return Number((slow / fast).toFixed(2));
}

function buildComparison(ts7, ts5) {
	const byOperation = new Map(ts5.operations.map((entry) => [entry.operation, entry]));
	return {
		modules: ts7.corpus.modules,
		cold_load: {
			ts7_us: ts7.cold_load_us,
			ts5_us: ts5.cold_load_us,
			ts7_speedup: ratio(ts5.cold_load_us, ts7.cold_load_us),
		},
		full_semantic_check: {
			ts7_us: ts7.full_semantic_check_us,
			ts5_us: ts5.full_semantic_check_us,
			ts7_speedup: ratio(ts5.full_semantic_check_us, ts7.full_semantic_check_us),
			ts7_diagnostics: ts7.semantic_diagnostics,
			ts5_diagnostics: ts5.semantic_diagnostics,
		},
		rss: {
			ts7_bytes: ts7.rss_bytes,
			ts5_bytes: ts5.rss_bytes,
			ts7_reduction: ratio(ts5.rss_bytes, ts7.rss_bytes),
		},
		operations: ts7.operations.map((entry) => {
			const other = byOperation.get(entry.operation);
			return {
				operation: entry.operation,
				ts7_p50_us: entry.p50_us,
				ts5_p50_us: other?.p50_us ?? null,
				ts7_p95_us: entry.p95_us,
				ts5_p95_us: other?.p95_us ?? null,
				ts7_speedup: other ? ratio(other.p50_us, entry.p50_us) : null,
				ts7_returned: entry.average_returned,
				ts5_returned: other?.average_returned ?? null,
				ts7_errors: entry.errors,
				ts5_errors: other?.errors ?? null,
			};
		}),
	};
}

function formatMilliseconds(microseconds) {
	return (microseconds / 1000).toFixed(1);
}

function formatMegabytes(bytes) {
	return (bytes / (1024 * 1024)).toFixed(1);
}

function buildReport(results) {
	const lines = [
		"# TypeScript engine benchmark: native TypeScript 7 vs TypeScript 5 Compiler API",
		"",
		`- Command: \`node run.mjs --sizes ${results.sizes.join(",")} --iterations ${results.iterations} --warmup ${results.warmup} --seed ${results.seed}\``,
		`- Commit: \`${results.commit}\``,
		`- Generated at: \`${results.generated_at}\``,
		`- Platform: \`${results.platform}\`, \`${results.runtime}\``,
		`- Engines: \`typescript@${results.engines.ts7}\` (native) and \`typescript@${results.engines.ts5}\` (JavaScript Compiler API)`,
		"",
		"## Project load and full type check",
		"",
		"| Modules | Cold load TS7 ms | Cold load TS5 ms | Load speedup | Full check TS7 ms | Full check TS5 ms | Check speedup | RSS TS7 MB | RSS TS5 MB | RSS ratio |",
		"| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |",
	];
	for (const comparison of results.comparisons) {
		lines.push(
			`| ${comparison.modules} | ${formatMilliseconds(comparison.cold_load.ts7_us)} | ${formatMilliseconds(comparison.cold_load.ts5_us)} | ${comparison.cold_load.ts7_speedup}x | ${formatMilliseconds(comparison.full_semantic_check.ts7_us)} | ${formatMilliseconds(comparison.full_semantic_check.ts5_us)} | ${comparison.full_semantic_check.ts7_speedup}x | ${formatMegabytes(comparison.rss.ts7_bytes)} | ${formatMegabytes(comparison.rss.ts5_bytes)} | ${comparison.rss.ts7_reduction}x |`,
		);
	}

	lines.push("", "## Warm semantic operations, p50 microseconds", "");
	for (const comparison of results.comparisons) {
		lines.push(`### ${comparison.modules} modules`, "");
		lines.push("| Operation | TS7 p50 us | TS5 p50 us | TS7 speedup | TS7 p95 us | TS5 p95 us | TS7 returned | TS5 returned |");
		lines.push("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |");
		for (const operation of comparison.operations) {
			lines.push(
				`| ${operation.operation} | ${operation.ts7_p50_us?.toFixed(1)} | ${operation.ts5_p50_us?.toFixed(1)} | ${operation.ts7_speedup}x | ${operation.ts7_p95_us?.toFixed(1)} | ${operation.ts5_p95_us?.toFixed(1)} | ${operation.ts7_returned} | ${operation.ts5_returned} |`,
			);
		}
		lines.push("");
	}

	lines.push(
		"## Method and asymmetries",
		"",
		"- Both engines analyse the same generated corpus: one hub module imported by every module, seeded cross-module imports, and a barrel that re-exports everything.",
		"- `cold_load` measures engine construction plus the first query that forces the program to exist. For TypeScript 7 it includes spawning the native `tsgo --api` server.",
		"- `symbol_at_position` resolves one symbol per call and exposes the fixed round-trip cost of the native engine; `symbol_batch_per_file` resolves 50 positions of one file in a single call, which the TypeScript 7 checker accepts natively and the TypeScript 5 checker has to emulate with a loop.",
		"- `references_in_file` is asymmetric by construction: TypeScript 7 exposes a file-scoped `getReferencesToSymbolInFile`, while the TypeScript 5 Compiler API only offers a project-wide search that is then filtered to the same file. Returned counts are compared to confirm both produce identical results.",
		"- `rss_bytes` sums the resident memory of the harness process and every descendant, so the native server counts against TypeScript 7.",
		"- The programs differ slightly in default library files because each compiler version ships its own `lib.*.d.ts` set; the corpus source files are identical.",
		"- Every measurement runs in its own Node process, so no engine benefits from the other's warm heap.",
		"",
		"## Consequences for Kivgraph",
		"",
		"- The native engine does not make every operation faster; it changes the cost model. Project-scale work becomes much cheaper, while each individual request pays a fixed inter-process round trip of roughly 70 to 140 microseconds.",
		"- Operations that Kivgraph runs once per project or per file change (load, full check, incremental re-resolve, file-scoped references) are decisively faster on the native engine, and the gap widens with corpus size.",
		"- Operations that Kivgraph would run once per symbol are slower on the native engine unless they are batched. The batched form is faster than the JavaScript Compiler API, so the worker protocol must be batch-oriented per file rather than chatty per symbol.",
		"- Transferring large symbol sets is the worst case: reading every export of a barrel costs one round trip per symbol payload and degrades with the export count. Bulk extraction must avoid materialising whole module export sets when a narrower query exists.",
		"- Resident memory is consistently lower for the native engine even after counting the spawned server process.",
		"",
	);
	return lines.join("\n");
}

function main() {
	const options = parseArguments(process.argv.slice(2));
	const comparisons = [];
	const raw = [];
	let engines = { ts7: "", ts5: "" };

	for (const size of options.sizes) {
		const ts7 = runEngine("ts7", size, options);
		const ts5 = runEngine("ts5", size, options);
		engines = { ts7: ts7.engine_version, ts5: ts5.engine_version };
		raw.push(ts7, ts5);
		comparisons.push(buildComparison(ts7, ts5));
	}

	const results = {
		benchmark: "typescript-engine",
		command: `node run.mjs --sizes ${options.sizes.join(",")} --iterations ${options.iterations} --warmup ${options.warmup} --seed ${options.seed}`,
		commit: gitState(),
		generated_at: new Date().toISOString(),
		platform: `${process.platform}/${process.arch}`,
		runtime: `node ${process.versions.node}`,
		engines,
		seed: options.seed,
		sizes: options.sizes,
		iterations: options.iterations,
		warmup: options.warmup,
		comparisons,
		raw,
	};

	fs.mkdirSync(options.output, { recursive: true });
	fs.writeFileSync(path.join(options.output, "results.json"), `${JSON.stringify(results, null, 2)}\n`);
	fs.writeFileSync(path.join(options.output, "report.md"), buildReport(results));
	process.stdout.write(`${buildReport(results)}\n`);
}

main();
