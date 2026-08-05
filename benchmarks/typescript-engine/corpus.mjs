import fs from "node:fs";
import path from "node:path";

const CORPUS_SCHEMA_VERSION = "001";

function mulberry32(seed) {
	let state = seed >>> 0;
	return () => {
		state = (state + 0x6d2b79f5) >>> 0;
		let value = Math.imul(state ^ (state >>> 15), 1 | state);
		value = (value + Math.imul(value ^ (value >>> 7), 61 | value)) ^ value;
		return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
	};
}

function moduleName(index) {
	return `mod${String(index).padStart(5, "0")}`;
}

/**
 * Writes a deterministic TypeScript corpus: one hub module imported everywhere,
 * a chain of cross-importing modules, and a barrel that re-exports all of them.
 * Returns the probe manifest used by every engine so both measure identical work.
 */
export function generateCorpus({ root, files, seed }) {
	fs.rmSync(root, { recursive: true, force: true });
	const sourceDir = path.join(root, "src");
	fs.mkdirSync(sourceDir, { recursive: true });

	fs.writeFileSync(
		path.join(root, "tsconfig.json"),
		`${JSON.stringify(
			{
				compilerOptions: {
					module: "nodenext",
					target: "esnext",
					strict: true,
					allowImportingTsExtensions: true,
					noEmit: true,
					skipLibCheck: true,
				},
				include: ["src"],
			},
			null,
			2,
		)}\n`,
	);

	fs.writeFileSync(
		path.join(sourceDir, "hub.ts"),
		["export function hub(value: number): number {", "\treturn value * 2;", "}", ""].join("\n"),
	);

	const random = mulberry32(seed);
	const names = [];
	for (let index = 0; index < files; index++) {
		const name = moduleName(index);
		names.push(name);
		const dependencies = [];
		const wanted = index === 0 ? 0 : 1 + Math.floor(random() * Math.min(3, index));
		while (dependencies.length < wanted) {
			const candidate = Math.floor(random() * index);
			if (!dependencies.includes(candidate)) {
				dependencies.push(candidate);
			}
		}
		dependencies.sort((left, right) => left - right);

		const lines = [`import { hub } from "./hub.ts";`];
		for (const dependency of dependencies) {
			lines.push(`import { handler${String(dependency).padStart(5, "0")} } from "./${moduleName(dependency)}.ts";`);
		}
		lines.push("");
		lines.push(`export interface Options${String(index).padStart(5, "0")} {`);
		lines.push("\treadonly value: number;");
		lines.push("\treadonly label: string;");
		lines.push("}");
		lines.push("");
		lines.push(`export function handler${String(index).padStart(5, "0")}(options: Options${String(index).padStart(5, "0")}): number {`);
		lines.push("\tlet total = hub(options.value);");
		for (const dependency of dependencies) {
			lines.push(
				`\ttotal += handler${String(dependency).padStart(5, "0")}({ value: options.value, label: options.label });`,
			);
		}
		lines.push("\treturn total + options.label.length;");
		lines.push("}");
		lines.push("");
		fs.writeFileSync(path.join(sourceDir, `${name}.ts`), lines.join("\n"));
	}
	const barrel = names.map((name) => `export * from "./${name}.ts";`).join("\n");
	fs.writeFileSync(path.join(sourceDir, "index.ts"), `export * from "./hub.ts";\n${barrel}\n`);

	const widePath = path.join(sourceDir, "wide.ts");
	const wideModules = names.slice(0, Math.min(50, names.length));
	const wideLines = wideModules.map(
		(name, index) => `import { handler${String(index).padStart(5, "0")} } from "./${name}.ts";`,
	);
	wideLines.push("");
	wideLines.push("export function wide(value: number, label: string): number {");
	wideLines.push("\tlet total = 0;");
	for (let index = 0; index < wideModules.length; index++) {
		wideLines.push(`\ttotal += handler${String(index).padStart(5, "0")}({ value, label });`);
	}
	wideLines.push("\treturn total;");
	wideLines.push("}");
	wideLines.push("");
	fs.writeFileSync(widePath, wideLines.join("\n"));

	return {
		schema_version: CORPUS_SCHEMA_VERSION,
		seed,
		files: files + 3,
		modules: files,
		root,
		configPath: path.join(root, "tsconfig.json"),
		barrelFile: path.join(sourceDir, "index.ts"),
		probes: { ...buildProbes(sourceDir, names), wide: buildWideProbes(widePath, wideModules.length) },
	};
}

function buildWideProbes(widePath, count) {
	const text = fs.readFileSync(widePath, "utf8");
	const positions = [];
	let cursor = 0;
	for (let index = 0; index < count; index++) {
		const marker = `\ttotal += handler${String(index).padStart(5, "0")}(`;
		const offset = text.indexOf(marker, cursor);
		if (offset < 0) {
			break;
		}
		positions.push(offset + "\ttotal += ".length);
		cursor = offset + marker.length;
	}
	return { file: widePath, positions };
}

function buildProbes(sourceDir, names) {
	const probeCount = Math.min(20, names.length);
	const step = Math.max(1, Math.floor(names.length / probeCount));
	const aliasCalls = [];
	const hubCalls = [];
	let editFile = null;

	for (let index = 0; index < names.length && aliasCalls.length < probeCount; index += step) {
		const filePath = path.join(sourceDir, `${names[index]}.ts`);
		const text = fs.readFileSync(filePath, "utf8");

		const hubOffset = text.indexOf("hub(options.value)");
		if (hubOffset >= 0) {
			hubCalls.push({ file: filePath, position: hubOffset });
		}

		const callOffset = text.indexOf("\ttotal += handler");
		if (callOffset >= 0) {
			aliasCalls.push({ file: filePath, position: callOffset + "\ttotal += ".length });
			if (editFile === null) {
				editFile = filePath;
			}
		}
	}

	if (editFile === null) {
		editFile = path.join(sourceDir, `${names[names.length - 1]}.ts`);
	}
	return { aliasCalls, hubCalls, editFile };
}
