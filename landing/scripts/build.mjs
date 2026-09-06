import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const astroPackage = require.resolve("astro/package.json");
const astroCLI = join(dirname(astroPackage), "bin", "astro.mjs");
const landingDirectory = fileURLToPath(new URL("../", import.meta.url));

const child = spawn(
  process.execPath,
  [astroCLI, "build", ...process.argv.slice(2)],
  {
    cwd: landingDirectory,
    env: process.env,
    stdio: ["inherit", "pipe", "pipe"],
  },
);

let output = "";
for (const [stream, destination] of [
  [child.stdout, process.stdout],
  [child.stderr, process.stderr],
]) {
  stream.on("data", (chunk) => {
    output += chunk;
    destination.write(chunk);
  });
}

const { code, signal } = await new Promise((resolve, reject) => {
  child.on("error", reject);
  child.on("close", (exitCode, exitSignal) =>
    resolve({ code: exitCode, signal: exitSignal }),
  );
});

if (signal) {
  console.error(`Astro build stopped by signal ${signal}.`);
  process.exitCode = 1;
} else if (code !== 0) {
  process.exitCode = code ?? 1;
} else if (/\[WARN\]/u.test(output)) {
  console.error(
    "Astro build completed with warnings; the landing build must be clean.",
  );
  process.exitCode = 1;
}
