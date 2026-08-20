import { mkdir } from "node:fs/promises";
import { join, resolve } from "node:path";

const nativeDir = resolve(import.meta.dir, "..");
const backendDir = resolve(nativeDir, "../backend");
const outputDir = join(nativeDir, "src-tauri/binaries");

const requestedTarget = process.argv[2] ?? process.env.TAURI_ENV_TARGET_TRIPLE ?? process.env.TARGET;
const target = requestedTarget || hostTarget();
const goTarget = targetToGo(target);
const output = join(outputDir, `ccplant-${target}`);

await mkdir(outputDir, { recursive: true });

const command = Bun.spawn(
  ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", output, "./main.go"],
  {
    cwd: backendDir,
    env: { ...process.env, CGO_ENABLED: "0", GOOS: goTarget.os, GOARCH: goTarget.arch },
    stdout: "inherit",
    stderr: "inherit",
  },
);

const exitCode = await command.exited;
if (exitCode !== 0) process.exit(exitCode);
console.log(`Built ${output}`);

function hostTarget(): string {
  if (process.platform === "darwin") {
    return process.arch === "arm64" ? "aarch64-apple-darwin" : "x86_64-apple-darwin";
  }
  if (process.platform === "linux" && process.arch === "x64") return "x86_64-unknown-linux-gnu";
  if (process.platform === "linux" && process.arch === "arm64") return "aarch64-unknown-linux-gnu";
  throw new Error(`Unsupported sidecar build host: ${process.platform}/${process.arch}`);
}

function targetToGo(value: string): { os: string; arch: string } {
  if (value === "aarch64-apple-darwin") return { os: "darwin", arch: "arm64" };
  if (value === "x86_64-apple-darwin") return { os: "darwin", arch: "amd64" };
  if (value === "aarch64-unknown-linux-gnu") return { os: "linux", arch: "arm64" };
  if (value === "x86_64-unknown-linux-gnu") return { os: "linux", arch: "amd64" };
  throw new Error(`Unsupported Tauri target triple: ${value}`);
}
