#!/usr/bin/env node
// Launcher: runs the Go binary from the matching @c0dn/ocws-<os>-<arch>
// package, or from vendor/ when install.js had to download it.
"use strict";
const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const path = require("node:path");

const exe = process.platform === "win32" ? "ocws.exe" : "ocws";

const vendored = path.join(__dirname, "..", "vendor", exe);

function findBinary() {
  if (process.env.OCWS_BINARY) return process.env.OCWS_BINARY;
  try {
    return require.resolve(`@c0dn/ocws-${process.platform}-${process.arch}/bin/${exe}`);
  } catch {}
  return existsSync(vendored) ? vendored : null;
}

function resolveBinary() {
  let bin = findBinary();
  if (bin) return bin;
  // Package managers increasingly skip dependency install scripts (npm 11+,
  // pnpm 10, Bun), so run the checksum-verified download on first use.
  spawnSync(process.execPath, [path.join(__dirname, "..", "install.js")], { stdio: "inherit" });
  if ((bin = findBinary())) return bin;
  console.error(
    `ocws: no binary for ${process.platform}-${process.arch}.\n` +
      "Reinstall with optional dependencies enabled, run `node install.js` in this package,\n" +
      "or download a release from https://github.com/c0dn/ocws/releases and set OCWS_BINARY.",
  );
  process.exit(1);
}

const result = spawnSync(resolveBinary(), process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  console.error(`ocws: ${result.error.message}`);
  process.exit(1);
}
if (result.signal) process.kill(process.pid, result.signal);
process.exit(result.status ?? 1);
