#!/usr/bin/env node
// Builds npm packages into dist/npm:
//   @c0dn/ocws              launcher + postinstall fallback
//   @c0dn/ocws-<os>-<cpu>   one prebuilt Go binary each
// Usage: node npm/build.mjs <version> [--targets linux-x64,darwin-arm64] [--pack]
import { execFileSync } from "node:child_process";
import { chmodSync, cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);
const version = args.find((a, i) => !a.startsWith("--") && args[i - 1] !== "--targets")?.replace(/^v/, "");
if (!version) {
  console.error("usage: node npm/build.mjs <version> [--targets a,b] [--pack]");
  process.exit(2);
}
const all = ["linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64", "win32-x64", "win32-arm64"];
const targets = args.includes("--targets") ? args[args.indexOf("--targets") + 1].split(",") : all;
const pack = args.includes("--pack");
const goos = { linux: "linux", darwin: "darwin", win32: "windows" };
const goarch = { x64: "amd64", arm64: "arm64" };
const out = join(root, "dist", "npm");
rmSync(out, { recursive: true, force: true });

const main = JSON.parse(readFileSync(join(root, "npm/ocws/package.json"), "utf8"));
main.version = version;
main.optionalDependencies = Object.fromEntries(all.map((t) => [`@c0dn/ocws-${t}`, version]));

for (const t of targets) {
  if (!all.includes(t)) throw new Error(`unknown target ${t}`);
  const [os, cpu] = t.split("-");
  const dir = join(out, `ocws-${t}`);
  const exe = os === "win32" ? "ocws.exe" : "ocws";
  mkdirSync(join(dir, "bin"), { recursive: true });
  execFileSync("go", ["build", "-trimpath", "-ldflags", `-s -w -X main.version=${version}`, "-o", join(dir, "bin", exe), "./cmd/ocws"], {
    cwd: root,
    stdio: "inherit",
    env: { ...process.env, CGO_ENABLED: "0", GOOS: goos[os], GOARCH: goarch[cpu] },
  });
  chmodSync(join(dir, "bin", exe), 0o755);
  cpSync(join(root, "LICENSE"), join(dir, "LICENSE"));
  const pkg = {
    name: `@c0dn/ocws-${t}`,
    version,
    description: `ocws binary for ${t}`,
    license: main.license,
    repository: main.repository,
    os: [os],
    cpu: [cpu],
    files: ["bin/", "LICENSE"],
    preferUnplugged: true,
  };
  writeFileSync(join(dir, "package.json"), JSON.stringify(pkg, null, 2) + "\n");
  console.log(`built @c0dn/ocws-${t}`);
}

const mainDir = join(out, "ocws");
cpSync(join(root, "npm/ocws"), mainDir, { recursive: true });
cpSync(join(root, "README.md"), join(mainDir, "README.md"));
cpSync(join(root, "LICENSE"), join(mainDir, "LICENSE"));
writeFileSync(join(mainDir, "package.json"), JSON.stringify(main, null, 2) + "\n");
console.log(`built @c0dn/ocws ${version}`);

if (pack) {
  for (const d of [...targets.map((t) => `ocws-${t}`), "ocws"]) {
    execFileSync("npm", ["pack", "--pack-destination", out], { cwd: join(out, d), stdio: ["ignore", "ignore", "inherit"] });
  }
  console.log(`packed tarballs into ${out}`);
}
