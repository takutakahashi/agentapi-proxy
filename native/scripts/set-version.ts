import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const version = (process.argv[2] ?? "").replace(/^v/, "");
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error(`Expected a semantic version, received: ${process.argv[2] ?? "<empty>"}`);
}

const configPath = resolve(import.meta.dir, "../src-tauri/tauri.conf.json");
const config = JSON.parse(await readFile(configPath, "utf8"));
config.version = version;
await writeFile(configPath, `${JSON.stringify(config, null, 2)}\n`);
console.log(`Set native app version to ${version}`);
