import fs from "node:fs";
import path from "node:path";
import { generateCorpus } from "./corpus.mjs";

function parseArguments(argv) {
	const options = { engine: "ts7", files: 200, seed: 42, iterations: 50, warmup: 5, corpusRoot: "" };
	for (let index = 0; index < argv.length; index++) {
		const value = argv[index + 1];
		switch (argv[index]) {
			case "--engine":
				options.engine = value;
				index++;
				break;
			case "--files":
				options.files = Number.parseInt(value, 10);
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
			case "--corpus-root":
				options.corpusRoot = value;
				index++;
				break;
			default:
				throw new Error(`unknown flag ${argv[index]}`);
		}
	}
	return options;
}

/** Sums resident memory of this process and every descendant, so the tsgo child counts. */
function processTreeRSS() {
	const self = process.pid;
	const children = new Map();
	for (const entry of fs.readdirSync("/proc")) {
		if (!/^\d+$/.test(entry)) {
			continue;
		}
		try {
			const status = fs.readFileSync(`/proc/${entry}/status`, "utf8");
			const parent = /^PPid:\s+(\d+)$/m.exec(status);
			const resident = /^VmRSS:\s+(\d+) kB$/m.exec(status);
			if (!parent) {
				continue;
			}
			children.set(Number.parseInt(entry, 10), {
				parent: Number.parseInt(parent[1], 10),
				rss: resident ? Number.parseInt(resident[1], 10) * 1024 : 0,
			});
		} catch {
			continue;
		}
	}
	let total = 0;
	const queue = [self];
	const seen = new Set();
	while (queue.length !== 0) {
		const pid = queue.pop();
		if (seen.has(pid)) {
			continue;
		}
		seen.add(pid);
		const record = children.get(pid);
		if (record) {
			total += record.rss;
		}
		for (const [candidate, info] of children) {
			if (info.parent === pid) {
				queue.push(candidate);
			}
		}
	}
	return total;
}

function measure(name, iterations, warmup, operation) {
	for (let index = 0; index < warmup; index++) {
		operation(index);
	}
	const samples = new Float64Array(iterations);
	let returned = 0;
	let errors = 0;
	for (let index = 0; index < iterations; index++) {
		const started = process.hrtime.bigint();
		let result = 0;
		try {
			result = operation(index) ?? 0;
		} catch {
			errors++;
		}
		samples[index] = Number(process.hrtime.bigint() - started) / 1000;
		returned += result;
	}
	const ordered = Array.from(samples).sort((left, right) => left - right);
	const percentile = (value) => ordered[Math.min(ordered.length - 1, Math.floor((value / 100) * ordered.length))];
	const total = ordered.reduce((accumulator, value) => accumulator + value, 0);
	return {
		operation: name,
		calls: iterations,
		errors,
		average_returned: returned / iterations,
		p50_us: percentile(50),
		p95_us: percentile(95),
		p99_us: percentile(99),
		max_us: ordered[ordered.length - 1],
		calls_per_s: total === 0 ? 0 : iterations / (total / 1_000_000),
	};
}

function timeOnce(operation) {
	const started = process.hrtime.bigint();
	const result = operation();
	return { us: Number(process.hrtime.bigint() - started) / 1000, result };
}

async function createTypeScript7Engine(corpus) {
	const { API, SymbolFlags } = await import("typescript-7/unstable/sync");
	const api = new API({ cwd: corpus.root });
	let snapshot = api.updateSnapshot({ openProjects: [corpus.configPath] });
	let project = snapshot.getProject(corpus.configPath) ?? snapshot.getProjects()[0];

	const resolveSymbol = (file, position) => project.checker.getSymbolAtPosition(file, position);

	return {
		name: "typescript-7-native",
		version: JSON.parse(
			fs.readFileSync(new URL("./node_modules/typescript-7/package.json", import.meta.url), "utf8"),
		).version,
		fileCount: () => project.program.getSourceFileNames().length,
		fullCheck: () => project.program.getSemanticDiagnostics().length,
		symbolAt: (file, position) => (resolveSymbol(file, position) ? 1 : 0),
		symbolAtBatch: (file, positions) =>
			project.checker.getSymbolAtPosition(file, positions).filter((symbol) => symbol !== undefined).length,
		aliasDeclarations: (file, position) => {
			const symbol = resolveSymbol(file, position);
			if (!symbol) {
				return 0;
			}
			const target = symbol.flags & SymbolFlags.Alias ? project.checker.getAliasedSymbol(symbol) : symbol;
			return target?.declarations?.length ?? 0;
		},
		referencesInFile: (file, position) => {
			const symbol = resolveSymbol(file, position);
			return symbol ? project.checker.getReferencesToSymbolInFile(file, symbol).length : 0;
		},
		exportsOfModule: (file) => {
			const sourceFile = project.program.getSourceFile(file);
			const symbol = sourceFile ? project.checker.getSymbolAtLocation(sourceFile) : undefined;
			return symbol ? project.checker.getExportsOfModule(symbol).length : 0;
		},
		applyEdit: (file, contents) => {
			fs.writeFileSync(file, contents);
			snapshot = api.updateSnapshot({
				openProjects: [corpus.configPath],
				fileChanges: { changedProjects: { [project.id]: { changedFiles: [file] } } },
			});
			project = snapshot.getProject(corpus.configPath) ?? snapshot.getProjects()[0];
		},
		close: () => api.close(),
	};
}

async function createTypeScript5Engine(corpus) {
	const typescript = (await import("typescript-5")).default;
	const configFile = typescript.readConfigFile(corpus.configPath, typescript.sys.readFile);
	const parsed = typescript.parseJsonConfigFileContent(
		configFile.config,
		typescript.sys,
		path.dirname(corpus.configPath),
	);

	const versions = new Map();
	const contents = new Map();
	const readFile = (fileName) => {
		if (contents.has(fileName)) {
			return contents.get(fileName);
		}
		return typescript.sys.readFile(fileName);
	};

	const host = {
		getScriptFileNames: () => parsed.fileNames,
		getScriptVersion: (fileName) => String(versions.get(fileName) ?? 0),
		getScriptSnapshot: (fileName) => {
			const text = readFile(fileName);
			return text === undefined ? undefined : typescript.ScriptSnapshot.fromString(text);
		},
		getCurrentDirectory: () => corpus.root,
		getCompilationSettings: () => parsed.options,
		getDefaultLibFileName: (options) => typescript.getDefaultLibFilePath(options),
		fileExists: typescript.sys.fileExists,
		readFile,
		readDirectory: typescript.sys.readDirectory,
		directoryExists: typescript.sys.directoryExists,
		getDirectories: typescript.sys.getDirectories,
	};

	const service = typescript.createLanguageService(host, typescript.createDocumentRegistry());
	let program = service.getProgram();
	let checker = program.getTypeChecker();

	const nodeAtPosition = (file, position) => {
		const sourceFile = program.getSourceFile(file);
		if (!sourceFile) {
			return undefined;
		}
		let found;
		const visit = (node) => {
			if (position < node.getStart(sourceFile) || position >= node.getEnd()) {
				return;
			}
			if (node.getChildCount(sourceFile) === 0) {
				found = node;
				return;
			}
			for (const child of node.getChildren(sourceFile)) {
				visit(child);
			}
		};
		visit(sourceFile);
		return found;
	};

	return {
		name: "typescript-5-compiler-api",
		version: typescript.version,
		fileCount: () => program.getSourceFiles().length,
		fullCheck: () => program.getSemanticDiagnostics().length,
		symbolAt: (file, position) => {
			const node = nodeAtPosition(file, position);
			return node && checker.getSymbolAtLocation(node) ? 1 : 0;
		},
		symbolAtBatch: (file, positions) => {
			let resolved = 0;
			for (const position of positions) {
				const node = nodeAtPosition(file, position);
				if (node && checker.getSymbolAtLocation(node)) {
					resolved++;
				}
			}
			return resolved;
		},
		aliasDeclarations: (file, position) => {
			const node = nodeAtPosition(file, position);
			const symbol = node ? checker.getSymbolAtLocation(node) : undefined;
			if (!symbol) {
				return 0;
			}
			const target = symbol.flags & typescript.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol;
			return target?.declarations?.length ?? 0;
		},
		referencesInFile: (file, position) => {
			const references = service.getReferencesAtPosition(file, position) ?? [];
			return references.filter((reference) => reference.fileName === file).length;
		},
		exportsOfModule: (file) => {
			const sourceFile = program.getSourceFile(file);
			const symbol = sourceFile ? checker.getSymbolAtLocation(sourceFile) : undefined;
			return symbol ? checker.getExportsOfModule(symbol).length : 0;
		},
		applyEdit: (file, text) => {
			fs.writeFileSync(file, text);
			contents.set(file, text);
			versions.set(file, (versions.get(file) ?? 0) + 1);
			program = service.getProgram();
			checker = program.getTypeChecker();
		},
		close: () => service.dispose(),
	};
}

async function main() {
	const options = parseArguments(process.argv.slice(2));
	const root = options.corpusRoot || path.join(process.env.TMPDIR ?? "/tmp", `kivgraph-ts-corpus-${options.files}-${options.seed}`);
	const corpus = generateCorpus({ root, files: options.files, seed: options.seed });

	const rssBefore = processTreeRSS();
	const load = await timeOnce(async () =>
		options.engine === "ts7" ? createTypeScript7Engine(corpus) : createTypeScript5Engine(corpus),
	);
	const engine = await load.result;
	const ready = timeOnce(() => engine.fileCount());
	const check = timeOnce(() => engine.fullCheck());

	const aliasProbes = corpus.probes.aliasCalls;
	const hubProbes = corpus.probes.hubCalls;
	const editFile = corpus.probes.editFile;
	const originalEdit = fs.readFileSync(editFile, "utf8");
	let editCounter = 0;

	const operations = [
		measure("symbol_at_position", options.iterations, options.warmup, (index) => {
			const probe = aliasProbes[index % aliasProbes.length];
			return engine.symbolAt(probe.file, probe.position);
		}),
		measure("symbol_batch_per_file", options.iterations, options.warmup, () =>
			engine.symbolAtBatch(corpus.probes.wide.file, corpus.probes.wide.positions),
		),
		measure("alias_declarations", options.iterations, options.warmup, (index) => {
			const probe = aliasProbes[index % aliasProbes.length];
			return engine.aliasDeclarations(probe.file, probe.position);
		}),
		measure("references_in_file", Math.min(options.iterations, 10), Math.min(options.warmup, 2), (index) => {
			const probe = hubProbes[index % hubProbes.length];
			return engine.referencesInFile(probe.file, probe.position);
		}),
		measure("exports_of_barrel", Math.min(options.iterations, 10), Math.min(options.warmup, 2), () =>
			engine.exportsOfModule(corpus.barrelFile),
		),
		measure("incremental_edit_and_resolve", Math.min(options.iterations, 10), Math.min(options.warmup, 2), () => {
			editCounter++;
			engine.applyEdit(editFile, `${originalEdit}\nexport const probe${editCounter} = ${editCounter};\n`);
			const probe = aliasProbes[0];
			return engine.symbolAt(probe.file, probe.position);
		}),
	];

	const rssAfter = processTreeRSS();
	fs.writeFileSync(editFile, originalEdit);
	engine.close();

	process.stdout.write(
		`${JSON.stringify({
			engine: engine.name,
			engine_version: engine.version,
			runtime: `node ${process.versions.node}`,
			corpus: {
				schema_version: corpus.schema_version,
				seed: corpus.seed,
				modules: corpus.modules,
				corpus_files: corpus.files,
				program_files: ready.result,
			},
			cold_load_us: load.us + ready.us,
			full_semantic_check_us: check.us,
			semantic_diagnostics: check.result,
			rss_bytes: rssAfter,
			rss_delta_bytes: rssAfter - rssBefore,
			operations,
		})}\n`,
	);
}

await main();
