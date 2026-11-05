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
    join(project, "cmd"),
    join(project, "internal/remote/browser/dist/static"),
    join(reference, "src/remote/browser/dist/static"),
  ])
    mkdirSync(dir, { recursive: true });
  const source = join(reference, "src/main.ts");
  writeFileSync(source, "export const value = 1;\n");
  writeFileSync(join(reference, "README.md"), "# Reference\n");
  writeFileSync(
    join(reference, "package.json"),
    JSON.stringify({ version: "1.2.3" }) + "\n",
  );
  writeFileSync(join(project, "README.md"), "# Port\n");
  mkdirSync(join(project, "internal/version"), { recursive: true });
  writeFileSync(
    join(project, "internal/version/version.go"),
    'package version\n\nvar Version = "1.2.3"\n',
  );
  const browser = "<html>browser</html>\n";
  const chunk = "export const chunk = 1;\n";
  writeFileSync(join(reference, "src/remote/browser/dist/index.html"), browser);
  writeFileSync(
    join(reference, "src/remote/browser/dist/static/app.js"),
    chunk,
  );
  writeFileSync(
    join(project, "internal/remote/browser/dist/index.html"),
    browser,
  );
  writeFileSync(
    join(project, "internal/remote/browser/dist/static/app.js"),
    chunk,
  );
  writeFileSync(join(project, "cmd/main.go"), "package main\n");
  const ledger = [
    "| `README.md` | `README.md` | Corrected | ported |",
    "| `src/main.ts` | `cmd/main.go` | Corrected | audited |",
    "| `src/remote/browser/dist/index.html` | `internal/remote/browser/dist/index.html` | Synchronized artifact | delivered |",
    "| `src/remote/browser/dist/static/app.js` | `internal/remote/browser/dist/static/*.js` | Synchronized artifact | delivered |",
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

  // Markdown table padding must not change the parsed ledger.
  writeFileSync(
    join(project, "ALIGNMENT.md"),
    [
      "| `README.md`                                      | `README.md`        | Corrected             | ported |",
      "| `src/main.ts`                                   | `cmd/main.go`      | Corrected             | audited |",
      "| `src/remote/browser/dist/index.html`           | `internal/remote/browser/dist/index.html` | Synchronized artifact | delivered |",
      "| `src/remote/browser/dist/static/app.js`        | `internal/remote/browser/dist/static/*.js` | Synchronized artifact | delivered |",
    ].join("\n"),
  );
  assert.equal(run().status, 0);
  writeFileSync(join(project, "ALIGNMENT.md"), ledger);

  mkdirSync(join(reference, "docs"), { recursive: true });
  writeFileSync(join(reference, "docs/notes.md"), "unrelated commit\n");
  git("add", "docs/notes.md");
  commit();
  assert.equal(run().status, 0);
  assert.equal(run("--write").status, 0);
  assert.equal(
    readFileSync(join(project, "alignment-inventory.json"), "utf8"),
    snapshot,
  );

  // A tracked README change fails until the ledger snapshot is refreshed.
  writeFileSync(join(reference, "README.md"), "# Reference v2\n");
  git("add", "README.md");
  commit();
  let readmeResult = run();
  assert.equal(readmeResult.status, 1);
  assert.match(readmeResult.stderr, /README\.md/);
  assert.equal(run("--write").status, 0);
  assert.equal(run().status, 0);

  // Version drift between the reference package and the Go default fails.
  writeFileSync(
    join(reference, "package.json"),
    JSON.stringify({ version: "1.2.4" }) + "\n",
  );
  git("add", "package.json");
  commit();
  let versionResult = run();
  assert.equal(versionResult.status, 1);
  assert.match(versionResult.stderr, /Version drift/);
  writeFileSync(
    join(project, "internal/version/version.go"),
    'package version\n\nvar Version = "1.2.4"\n',
  );
  assert.equal(run("--write").status, 0);
  assert.equal(run().status, 0);

  writeFileSync(source, "export const value = 2;\n");
  let result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Reference src tree is dirty/);
  git("add", "src/main.ts");
  commit();
  result = run();
  assert.equal(result.status, 1);
  assert.match(
    result.stderr,
    /Reference hashes or ledger changed: src\/main.ts/,
  );
  writeFileSync(source, "export const value = 1;\n");
  git("add", "src/main.ts");
  commit();
  result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /reference_commit/);
  assert.doesNotMatch(result.stderr, /src\/main\.ts/);
  assert.equal(run("--write").status, 0);
  assert.equal(run().status, 0);

  writeFileSync(
    join(project, "ALIGNMENT.md"),
    ledger.replace(
      "| Corrected | audited |",
      "| Partially aligned | audited |",
    ),
  );
  result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /src\/main.ts/);
  writeFileSync(join(project, "ALIGNMENT.md"), ledger);

  rmSync(join(project, "cmd/main.go"));
  result = run();
  assert.equal(result.status, 1);
  assert.match(
    result.stderr,
    /Missing counterpart for src\/main\.ts: cmd\/main\.go/,
  );
  writeFileSync(join(project, "cmd/main.go"), "package main\n");

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
  commit();
  result = run();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Inventory mismatch: missing=\["src\/new.ts"\]/);
});
