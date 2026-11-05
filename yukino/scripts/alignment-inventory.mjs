import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { parseArgs, isDeepStrictEqual } from "node:util";
import { fileURLToPath } from "node:url";

const project = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function git(reference, ...args) {
  return execFileSync("git", ["-C", reference, ...args], { encoding: "utf8" });
}

function files(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory()
      ? files(path)
      : statSync(path).isFile()
        ? [path]
        : [];
  });
}

function inventory(reference) {
  const entries = readFileSync(join(project, "ALIGNMENT.md"), "utf8")
    .split(/\r?\n/u)
    .filter((line) => line.startsWith("| `src/"))
    .map((line) => {
      const fields = line.split(" | ");
      const source = fields[0].split("`")[1];
      const path = join(reference, source);
      if (!statSync(path).isFile()) {
        throw new Error(`Missing reference file: ${source}`);
      }
      return {
        source,
        source_sha256: createHash("sha256")
          .update(readFileSync(path))
          .digest("hex"),
        counterpart: fields[1],
        status: fields[2],
      };
    });
  const tracked = git(reference, "ls-files", "-z", "src")
    .split("\0")
    .filter(Boolean);
  const built = files(join(reference, "src/remote/browser/dist")).map((path) =>
    relative(reference, path).split("\\").join("/"),
  );
  const recorded = new Set(entries.map((entry) => entry.source));
  if (recorded.size !== entries.length)
    throw new Error("Duplicate ledger rows");
  const expected = new Set([...tracked, ...built]);
  const missing = [...expected].filter((path) => !recorded.has(path)).sort();
  const extra = [...recorded].filter((path) => !expected.has(path)).sort();
  if (missing.length || extra.length) {
    throw new Error(
      `Inventory mismatch: missing=${JSON.stringify(missing)}, extra=${JSON.stringify(extra)}`,
    );
  }
  for (const path of built) {
    const local = join(
      project,
      "internal/remote/browser/dist",
      relative("src/remote/browser/dist", path),
    );
    if (!readFileSync(local).equals(readFileSync(join(reference, path)))) {
      throw new Error(`Browser artifact differs: ${path}`);
    }
  }
  const localArtifacts = files(join(project, "internal/remote/browser/dist"))
    .map((path) =>
      relative(join(project, "internal/remote/browser/dist"), path),
    )
    .sort();
  const referenceArtifacts = built
    .map((path) => relative("src/remote/browser/dist", path))
    .sort();
  if (!isDeepStrictEqual(localArtifacts, referenceArtifacts)) {
    throw new Error("Browser artifact file sets differ");
  }
  const counts = {};
  for (const { status } of entries) counts[status] = (counts[status] ?? 0) + 1;
  return {
    reference_commit: git(reference, "rev-parse", "HEAD").trim(),
    counts: Object.fromEntries(
      Object.entries(counts).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)),
    ),
    files: entries.sort((a, b) =>
      a.source < b.source ? -1 : a.source > b.source ? 1 : 0,
    ),
  };
}

try {
  const { values } = parseArgs({
    options: {
      reference: {
        type: "string",
        default: join(homedir(), "github/yukino-code/apps/yukino"),
      },
      write: { type: "boolean", default: false },
    },
  });
  const snapshot = join(project, "alignment-inventory.json");
  const result = inventory(resolve(values.reference));
  if (values.write) {
    writeFileSync(snapshot, JSON.stringify(result, null, 2) + "\n");
  } else {
    const recorded = JSON.parse(readFileSync(snapshot, "utf8"));
    if (
      !isDeepStrictEqual(recorded.files, result.files) ||
      !isDeepStrictEqual(recorded.counts, result.counts)
    ) {
      const previous = new Map(
        recorded.files.map((file) => [file.source, file]),
      );
      const current = new Map(result.files.map((file) => [file.source, file]));
      const changed = [...new Set([...previous.keys(), ...current.keys()])]
        .filter(
          (path) => !isDeepStrictEqual(previous.get(path), current.get(path)),
        )
        .sort();
      throw new Error(
        `Reference hashes or ledger changed: ${changed.join(", ") || "status counts"}; compare the changed files before refreshing the inventory`,
      );
    }
  }
  console.log(
    JSON.stringify({
      files: result.files.length,
      counts: result.counts,
      reference_commit: result.reference_commit,
    }),
  );
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
