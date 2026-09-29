// Reference copy of sha256Path from the original TS workspace-manifest engine
// (src/custom-tools/workspace-manifest/shared.ts). Used only by hash_test.go.
import { createHash } from "node:crypto";
import { promises as fs } from "node:fs";
import { join } from "node:path";

async function sha256File(p: string) { return createHash("sha256").update(await fs.readFile(p)).digest("hex"); }
async function sha256Directory(dir: string) {
  const hash = createHash("sha256");
  const visit = async (cur: string, prefix: string): Promise<void> => {
    const entries = await fs.readdir(cur, { withFileTypes: true });
    entries.sort((a, b) => a.name.localeCompare(b.name));
    for (const e of entries) {
      const p = join(cur, e.name);
      const rel = prefix ? `${prefix}/${e.name}` : e.name;
      if (e.isDirectory()) { hash.update(`dir ${rel}\n`); await visit(p, rel); continue; }
      if (e.isFile()) { hash.update(`file ${rel}\0`); hash.update(await sha256File(p)); hash.update("\n"); continue; }
      if (e.isSymbolicLink()) { hash.update(`symlink ${rel}\0${await fs.readlink(p)}\n`); continue; }
    }
  };
  hash.update("directory\n");
  await visit(dir, "");
  return hash.digest("hex");
}
async function sha256Path(p: string) { return (await fs.stat(p)).isDirectory() ? sha256Directory(p) : sha256File(p); }
for (const p of process.argv.slice(2)) console.log(await sha256Path(p));
