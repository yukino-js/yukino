import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  existsSync,
  globSync,
  readFileSync,
  readdirSync,
  statSync,
  writeFileSync,
} from "node:fs";
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

function ledgerRows(markdown) {
  return markdown
    .split(/\r?\n/u)
    .filter((line) => line.startsWith("| `"))
    .map((line) => {
      const cells = line.split("|");
      if (cells[0].trim() === "") cells.shift();
      if (cells.length && cells[cells.length - 1].trim() === "") cells.pop();
      const [sourceCell, counterpart, status] = cells.map((cell) =>
        cell.trim(),
      );
      const source = sourceCell.split("`")[1];
      if (!source || !counterpart || !status)
        throw new Error(`Malformed ledger row: ${line}`);
      return { source, counterpart, status };
    });
}

function checkCounterparts(entries) {
  for (const { source, counterpart } of entries) {
    const paths = [...counterpart.matchAll(/`([^`]+)`/gu)].map(
      (match) => match[1],
    );
    if (paths.length === 0)
      throw new Error(`Ledger row for ${source} has no counterpart path`);
    for (const target of paths) {
      const found = target.includes("*")
        ? globSync(target, { cwd: project }).length > 0
        : existsSync(join(project, target));
      if (!found)
        throw new Error(`Missing counterpart for ${source}: ${target}`);
    }
  }
}

const STATUSES = [
  "Corrected",
  "Language-specific difference",
  "Partially aligned",
  "Pending review",
  "Reviewed",
  "Shared browser build",
  "Synchronized artifact",
];

function referenceVersion(reference) {
  const pkg = JSON.parse(readFileSync(join(reference, "package.json"), "utf8"));
  if (typeof pkg.version !== "string" || pkg.version === "")
    throw new Error("Reference package.json has no version");
  return pkg.version;
}

function localVersion() {
  const source = readFileSync(
    join(project, "internal/version/version.go"),
    "utf8",
  );
  const match = /var Version = "([^"]*)"/u.exec(source);
  if (!match)
    throw new Error(
      'internal/version/version.go has no `var Version = "..."` default',
    );
  return match[1];
}

function inventory(reference) {
  const dirty = git(reference, "status", "--porcelain", "--", "src").trim();
  if (dirty)
    throw new Error(
      `Reference src tree is dirty; commit or restore it before inventorying:\n${dirty}`,
    );
  const referenceCommit = git(
    reference,
    "log",
    "-1",
    "--format=%H",
    "--",
    "src",
  ).trim();
  if (!referenceCommit)
    throw new Error("Reference repository has no commit touching src");
  const version = referenceVersion(reference);
  const goVersion = localVersion();
  if (version !== goVersion)
    throw new Error(
      `Version drift: reference package.json ${version} != internal/version/version.go ${goVersion}; align the Go default with the reference version`,
    );
  const entries = ledgerRows(
    readFileSync(join(project, "ALIGNMENT.md"), "utf8"),
  ).map(({ source, counterpart, status }) => {
    const path = join(reference, source);
    if (!statSync(path).isFile()) {
      throw new Error(`Missing reference file: ${source}`);
    }
    return {
      source,
      source_sha256: createHash("sha256")
        .update(readFileSync(path))
        .digest("hex"),
      counterpart,
      status,
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
  const expected = new Set([...tracked, ...built, "README.md"]);
  const missing = [...expected].filter((path) => !recorded.has(path)).sort();
  const extra = [...recorded].filter((path) => !expected.has(path)).sort();
  if (missing.length || extra.length) {
    throw new Error(
      `Inventory mismatch: missing=${JSON.stringify(missing)}, extra=${JSON.stringify(extra)}`,
    );
  }
  checkCounterparts(entries);
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
  const counts = Object.fromEntries(STATUSES.map((status) => [status, 0]));
  for (const { status } of entries) {
    if (!(status in counts))
      throw new Error(`Unknown ledger status: ${status}`);
    counts[status] += 1;
  }
  return {
    reference_commit: referenceCommit,
    reference_version: version,
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
      !isDeepStrictEqual(recorded.counts, result.counts) ||
      recorded.reference_commit !== result.reference_commit ||
      recorded.reference_version !== result.reference_version
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
      if (recorded.reference_commit !== result.reference_commit)
        changed.push(
          `reference_commit ${recorded.reference_commit} -> ${result.reference_commit}`,
        );
      if (recorded.reference_version !== result.reference_version)
        changed.push(
          `reference_version ${recorded.reference_version} -> ${result.reference_version}`,
        );
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
      reference_version: result.reference_version,
    }),
  );
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
