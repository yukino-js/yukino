import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import subprocess
import sys


def inventory(project, reference):
    entries = []
    for line in (project / "ALIGNMENT.md").read_text().splitlines():
        if not line.startswith("| `src/"):
            continue
        fields = line.split(" | ")
        source = fields[0].split("`")[1]
        path = reference / source
        if not path.is_file():
            raise ValueError(f"Missing reference file: {source}")
        entries.append({
            "source": source,
            "source_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "counterpart": fields[1],
            "status": fields[2],
        })
    tracked = set(subprocess.check_output(
        ["git", "-C", str(reference), "ls-files", "src"], text=True
    ).splitlines())
    built = {
        path.relative_to(reference).as_posix()
        for path in (reference / "src/remote/browser/dist").rglob("*")
        if path.is_file()
    }
    recorded = {entry["source"] for entry in entries}
    if len(recorded) != len(entries):
        raise ValueError("Duplicate ledger rows")
    if recorded != tracked | built:
        raise ValueError(f"Inventory mismatch: missing={sorted((tracked | built)-recorded)}, extra={sorted(recorded-(tracked | built))}")
    for path in built:
        local = project / "internal/remote/browser/dist" / Path(path).relative_to("src/remote/browser/dist")
        if not local.is_file() or local.read_bytes() != (reference / path).read_bytes():
            raise ValueError(f"Browser artifact differs: {path}")
    return {
        "reference_commit": subprocess.check_output(
            ["git", "-C", str(reference), "rev-parse", "HEAD"], text=True
        ).strip(),
        "counts": dict(sorted(Counter(entry["status"] for entry in entries).items())),
        "files": sorted(entries, key=lambda entry: entry["source"]),
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--reference", type=Path, default=Path.home() / "github/yukino-code/apps/yukino")
    parser.add_argument("--write", action="store_true")
    args = parser.parse_args()
    project = Path(__file__).resolve().parent.parent
    snapshot = project / "alignment-inventory.json"
    try:
        result = inventory(project, args.reference.resolve())
        if args.write:
            snapshot.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        elif json.loads(snapshot.read_text()) != result:
            raise ValueError("Reference hashes or ledger changed; compare the changed files before refreshing the inventory")
        print(json.dumps({"files": len(result["files"]), "counts": result["counts"]}))
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
