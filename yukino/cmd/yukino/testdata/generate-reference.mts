import fs from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";

const reference = resolve(process.env.YUKINO_REFERENCE ?? resolve(homedir(), "github/yukino-code/apps/yukino"));
const { parseCliArgs } = await import(pathToFileURL(resolve(reference, "src/cli.ts")).href);
const tokens = ["", " ", "task", "中文", "-p", "--print", "--help", "-h", "--version", "-v", "--", "--resume", "--resume=", "--resume=saved", "--remote", "--a2a", "9000", "--output-format", "text", "stream-json", "--bogus", "update", "--tag", "--tag=canary"];
const args = [[], ...tokens.map(t => [t]), ...tokens.flatMap(a => tokens.map(b => [a, b])), ...["-p", "--resume", "--remote", "--a2a", "--output-format"].flatMap(a => tokens.flatMap(b => tokens.map(c => [a, b, c]))), ["-p", "--", "--remote", "extra"], ["--output-format", "stream-json", "-p", "--", "--remote"], ["update", "--tag=a", "--tag=b"], ["--resume", "--output-format", "text"]];
const cases = args.map(args => {
  try { return { args, expected: parseCliArgs(args) }; }
  catch(error) { return { args, error: (error as Error).message }; }
});
fs.writeFileSync(new URL("cli-reference.json", import.meta.url), JSON.stringify({
  reference: { commit: execFileSync("git", ["-C", reference, "rev-parse", "HEAD"], {encoding: "utf8"}).trim(), sha256: createHash("sha256").update(fs.readFileSync(resolve(reference, "src/cli.ts"))).digest("hex") }, cases,
}, null, 2) + "\n");
