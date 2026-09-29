// Postinstall fallback. The normal path is the platform package pulled in via
// optionalDependencies, which works even when install scripts are disabled
// (Bun and pnpm defaults). Only when that package is missing do we download
// the release binary and verify it against the release checksums.txt.
"use strict";
const crypto = require("node:crypto");
const fs = require("node:fs");
const https = require("node:https");
const path = require("node:path");

const { version } = require("./package.json");
const exe = process.platform === "win32" ? "ocws.exe" : "ocws";
const platformPkg = `@c0dn/ocws-${process.platform}-${process.arch}`;
const goos = { linux: "linux", darwin: "darwin", win32: "windows" }[process.platform];
const goarch = { x64: "amd64", arm64: "arm64" }[process.arch];

function hasPlatformPackage() {
  try {
    require.resolve(`${platformPkg}/bin/${exe}`);
    return true;
  } catch {
    return false;
  }
}

function get(url, redirects = 5) {
  if (url.startsWith("file://")) return Promise.resolve(fs.readFileSync(new URL(url)));
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { "User-Agent": "ocws-npm-install" } }, (res) => {
        if ([301, 302, 303, 307, 308].includes(res.statusCode) && res.headers.location && redirects > 0) {
          res.resume();
          resolve(get(new URL(res.headers.location, url).toString(), redirects - 1));
          return;
        }
        if (res.statusCode !== 200) {
          res.resume();
          reject(new Error(`GET ${url}: HTTP ${res.statusCode}`));
          return;
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

async function main() {
  if (process.env.OCWS_SKIP_DOWNLOAD || hasPlatformPackage()) return;
  if (!goos || !goarch) {
    console.warn(`ocws: no prebuilt binary for ${process.platform}-${process.arch}; install with \`go install github.com/c0dn/ocws/cmd/ocws@latest\`.`);
    return;
  }
  const base = process.env.OCWS_DOWNLOAD_BASE || `https://github.com/c0dn/ocws/releases/download/v${version}`;
  const asset = `ocws_${goos}_${goarch}${goos === "windows" ? ".exe" : ""}`;
  const [binary, sums] = await Promise.all([get(`${base}/${asset}`), get(`${base}/checksums.txt`)]);
  const expected = sums
    .toString("utf8")
    .split("\n")
    .map((line) => line.trim().split(/\s+/))
    .find(([, name]) => name === asset)?.[0];
  const actual = crypto.createHash("sha256").update(binary).digest("hex");
  if (!expected || expected !== actual) {
    throw new Error(`checksum mismatch for ${asset} (expected ${expected ?? "none"}, got ${actual})`);
  }
  const dir = path.join(__dirname, "vendor");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, exe), binary, { mode: 0o755 });
}

main().catch((err) => {
  // Never fail the whole install; the launcher explains how to recover.
  console.warn(`ocws: could not download the ${platformPkg} binary: ${err.message}`);
});
