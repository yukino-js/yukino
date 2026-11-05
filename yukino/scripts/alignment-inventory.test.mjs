import assert from "node:assert/strict";
import { spawnSync, execFileSync } from "node:child_process";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

test("inventory checks content independently of unrelated commits", (t) => {
  const root = mkdtempSync(join(tmpdir(), "yukino-inventory-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const project = join(root, "port");
  const reference = join(root, "reference");
  for (const dir of [
    join(project, "scripts"),
    join(project, "internal/remote/browser/dist"),
    join(reference, "src/remote/browser/dist"),
  ])
    mkdirSync(dir, { recursive: true });
  const source = join(reference, "src/main.ts");
  writeFileSync(source, "export const value = 1;\n");
  const browser = "<html>browser</html>\n";
  writeFileSync(join(reference, "src/remote/browser/dist/index.html"), browser);
  writeFileSync(
    join(project, "internal/remote/browser/dist/index.html"),
    browser,
  );
  const ledger = [
    "| `src/main.ts` | `cmd/main.go` | Corrected | audited |",
    "| `src/remote/browser/dist/index.html` | `internal/remote/browser/dist/index.html` | Synchronized artifact | delivered |",
  ].join("\n");
  writeFileSync(join(project, "ALIGNMENT.md"), ledger);
  const script = join(project, "scripts/alignment-inventory.mjs");
  writeFileSync(
    script,
    readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), "alignment-inventory.mjs"),
    ),
  );
  const git = (...args) =>
    execFileSync("git", ["-C", reference, ...args], { stdio: "pipe" });
  git("init");
  git("add", ".");
  const commit = () =>
    git(
      "-c",
      "user.name=Inventory Test",
      "-c",
      "user.email=inventory@example.invalid",
      "commit",
      "-m",
      "fixture",
    );
  commit();
  const run = (...args) =>
    spawnSync(process.execPath, [script, "--reference", reference, ...args], {
      encoding: "utf8",
    });
  assert.equal(run("--write").status, 0);
  const snapshot = readFileSync(
    join(project, "alignment-inventory.json"),
    "utf8",
  );
  assert.equal(run().status, 0);

  writeFileSync(join(reference, "README.md"), "unrelated commit\n");
  git("add", "README.md");
  commit();
  assert.equal(run().status, 0);
  assert.equal(
    readFileSync(join(project, "alignment-inventory.json"), "utf8"),
    snapshot,
  );

  writeFileSync(source, "export const value = 2;\n");
  let result = run();
  assert.equal(result.status, 1);
  assert.match(
    result.stderr,
    /Reference hashes or ledger changed: src\/main.ts/,
  );
  writeFileSync(source, "export const value = 1;\n");

  writeFileSync(
    join(project, "ALIGNMENT.md"),
    ledger.replace("Corrected", "Partially aligned"),
  );
  result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /src\/main.ts/);
  writeFileSync(join(project, "ALIGNMENT.md"), ledger);

  const extra = join(project, "internal/remote/browser/dist/obsolete.js");
  writeFileSync(extra, "obsolete");
  result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Browser artifact file sets differ/);
  rmSync(extra);
  writeFileSync(
    join(project, "internal/remote/browser/dist/index.html"),
    "drift",
  );
  result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Browser artifact differs/);
  writeFileSync(
    join(project, "internal/remote/browser/dist/index.html"),
    browser,
  );

  writeFileSync(join(reference, "src/new.ts"), "export {};\n");
  git("add", "src/new.ts");
  result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Inventory mismatch: missing=\["src\/new.ts"\]/);
});
