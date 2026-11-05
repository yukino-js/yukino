import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const project = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const reference =
  process.argv[2] ?? path.join(os.homedir(), "github/yukino-code/apps/yukino");
const require = createRequire(path.join(reference, "package.json"));
const z = require("zod");
const source = fs.readFileSync(
  path.join(reference, "src/remote/server.ts"),
  "utf8",
);
const declarations = source.slice(
  source.indexOf("const UserMessageSchema"),
  source.indexOf("/** Serves a static file"),
);
const entries = [
  ["user_message", "UserMessageSchema", { content: "hello" }],
  [
    "permission_response",
    "PermissionResponseSchema",
    { id: "id", response: "allow" },
  ],
  [
    "ask_user_response",
    "AskUserResponseSchema",
    { id: "id", answers: { Q: "A" } },
  ],
  [
    "plan_approval_response",
    "PlanApprovalResponseSchema",
    { id: "id", choice: "feedback", feedback: "fix" },
  ],
  ["set_permission_mode", "SetPermissionModeSchema", { mode: "plan" }],
  ["model_select", "ModelSelectSchema", { model: "model" }],
  ["provider_select", "ProviderSelectSchema", { index: 0 }],
  [
    "provider_save",
    "ProviderSaveSchema",
    {
      name: "p",
      protocol: "openai",
      base_url: "https://example.com",
      api_key: "key",
      model: "m",
      thinking: "high",
    },
  ],
  [
    "rewind_action",
    "RewindActionSchema",
    { snapshotId: "snap", action: "code_only" },
  ],
  [
    "file_complete_request",
    "FileCompleteRequestSchema",
    { requestId: "id", query: "main" },
  ],
  [
    "image_upload",
    "ImageUploadSchema",
    { clientId: "id", name: "test.png", dataBase64: "aGVsbG8=" },
  ],
];
const schemaNames = entries.map(([, schema]) => schema).join(",");
const schemas = new Function("z", `${declarations}; return {${schemaNames}};`)(
  z,
);
const cases = [];
for (const [kind, name, data] of entries) {
  const add = (label, payload) =>
    cases.push({
      name: `${kind}/${label}`,
      kind,
      data: payload,
      valid: schemas[name].safeParse(payload).success,
    });
  add("valid", data);
  for (const bad of [null, [], false, "data", 1])
    add(`object-${JSON.stringify(bad)}`, bad);
  for (const key of Object.keys(data)) {
    const omitted = { ...data };
    delete omitted[key];
    add(`${key}-missing`, omitted);
    for (const value of [null, {}, [], true, -1, 1.5, "", "bogus"])
      add(`${key}-${JSON.stringify(value)}`, { ...data, [key]: value });
  }
}
for (const [kind, key, values] of [
  ["permission_response", "response", ["allow", "deny", "allowAlways"]],
  [
    "set_permission_mode",
    "mode",
    ["default", "acceptEdits", "bypassPermissions", "plan"],
  ],
  ["plan_approval_response", "choice", ["yolo", "manual", "feedback"]],
  [
    "rewind_action",
    "action",
    ["code_and_conversation", "conversation_only", "code_only"],
  ],
  ["provider_save", "protocol", ["anthropic", "openai", "openai-compat"]],
  [
    "provider_save",
    "thinking",
    ["off", "minimal", "low", "medium", "high", "xhigh", "max"],
  ],
]) {
  const [, schema, base] = entries.find(([entryKind]) => entryKind === kind);
  for (const value of values) {
    const data = { ...base, [key]: value };
    cases.push({
      name: `${kind}/${key}=${value}`,
      kind,
      data,
      valid: schemas[schema].safeParse(data).success,
    });
  }
}
const write = (name, values) =>
  fs.writeFileSync(
    path.join(project, "internal/remote/testdata", name),
    `[
${values.map((value) => `  ${JSON.stringify(value)}`).join(",\n")}
]\n`,
  );
write("m44-ws-validation.json", cases);
const inputs = [
  "",
  "a",
  "ab",
  "abc",
  "abcd",
  "abcde",
  "abc===tail",
  "@@@",
  "a G\nVsbG8=",
  "aGVsbG8",
  "aGVsbG8=ignored",
  "aGVsbG8$!",
  "__--",
  "a===",
  "YQ====",
  "YQ\u0000==",
];
write(
  "m44-base64.json",
  inputs.map((input) => ({
    input,
    output: Buffer.from(input, "base64").toString("base64"),
  })),
);
process.stdout.write(
  `${cases.length} schema cases and ${inputs.length} base64 cases\n`,
);
