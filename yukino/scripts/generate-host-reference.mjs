import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { EventEmitter } from "node:events";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

const { values } = parseArgs({
  options: {
    reference: {
      type: "string",
      default: join(homedir(), "github/yukino-code/apps/yukino"),
    },
  },
});
const reference = resolve(values.reference);
const { RemoteServer } = await import(
  pathToFileURL(join(reference, "src/remote/server.ts"))
);
const { ConversationManager } = await import(
  pathToFileURL(join(reference, "src/conversation/index.ts"))
);
const server = () =>
  new RemoteServer({ providers: [], addr: "0", enableCoordinatorMode: false });
const templates = {
  user_message: { content: "hello" },
  permission_response: { id: "request", response: "allowAlways" },
  ask_user_response: { id: "request", answers: { question: "answer" } },
  plan_approval_response: {
    id: "request",
    choice: "feedback",
    feedback: "change this",
  },
  set_permission_mode: { mode: "plan" },
  model_select: { model: "model" },
  provider_select: { index: 0 },
  provider_save: {
    name: "provider",
    protocol: "openai",
    base_url: "http://localhost",
    api_key: "key",
    model: "model",
    thinking: "low",
  },
  rewind_action: { snapshotId: "snapshot", action: "conversation_only" },
  file_complete_request: { requestId: "request", query: "src" },
  image_upload: { clientId: "image", name: "image.png", dataBase64: "AA==" },
};
const cases = [];
for (const [type, data] of Object.entries(templates)) {
  cases.push({ label: `${type}/valid`, input: { type, data } });
  cases.push({ label: `${type}/no-data`, input: { type } });
  for (const field of Object.keys(data)) {
    const missing = { ...data };
    delete missing[field];
    cases.push({
      label: `${type}/missing-${field}`,
      input: { type, data: missing },
    });
    cases.push({
      label: `${type}/null-${field}`,
      input: { type, data: { ...data, [field]: null } },
    });
    cases.push({
      label: `${type}/wrong-${field}`,
      input: { type, data: { ...data, [field]: [] } },
    });
  }
}
for (const index of [-1, 0.5, 9007199254740992])
  cases.push({
    label: `provider_select/index-${index}`,
    input: { type: "provider_select", data: { index } },
  });
for (const [type, field] of [
  ["permission_response", "response"],
  ["plan_approval_response", "choice"],
  ["set_permission_mode", "mode"],
  ["provider_save", "protocol"],
  ["provider_save", "thinking"],
  ["rewind_action", "action"],
]) {
  cases.push({
    label: `${type}/invalid-${field}`,
    input: { type, data: { ...templates[type], [field]: "unknown" } },
  });
}
for (const answers of [{ question: null }, { question: 1 }, []])
  cases.push({
    label: `ask_user_response/answers-${JSON.stringify(answers)}`,
    input: { type: "ask_user_response", data: { id: "request", answers } },
  });
for (const type of [
  "cancel",
  "ping",
  "model_list_request",
  "provider_list_request",
  "rewind_list_request",
  "background_shells",
]) {
  cases.push({ label: `${type}/no-data`, input: { type } });
  cases.push({ label: `${type}/valid`, input: { type, data: null } });
}
for (const input of [
  null,
  [],
  {},
  { type: null },
  { type: 2 },
  { type: "unknown", data: {} },
])
  cases.push({ label: `envelope/${JSON.stringify(input)}`, input });

for (const entry of cases) {
  const s = server();
  let called = false;
  let pending;
  s.send = () => {
    called = true;
  };
  s.broadcast = () => {};
  const methods = [
    "handleUserMessage",
    "handleSteeringMessage",
    "handlePlanApprovalResponse",
    "cancelActiveRun",
    "applyPermissionMode",
    "broadcastModelList",
    "applyModel",
    "broadcastProviderList",
    "applyProviderSwitch",
    "handleProviderSave",
    "broadcastRewindList",
    "handleRewindAction",
    "sendFileCompletions",
    "handleImageUpload",
    "backgroundForegroundShells",
  ];
  for (const method of methods)
    s[method] = () => {
      called = true;
    };
  s.changeProviderConfiguration = async (change) => {
    await change();
  };
  const message = s.handleWsMessage.bind(s);
  s.handleWsMessage = (...args) => {
    pending = message(...args);
  };
  const ws = new EventEmitter();
  s.wss.emit("connection", ws);
  s.pendingPermissions = {
    get: () => {
      called = true;
    },
    delete: () => false,
  };
  s.pendingAsks = {
    get: () => {
      called = true;
    },
    delete: () => false,
  };
  called = false;
  ws.emit("message", Buffer.from(JSON.stringify(entry.input)));
  await pending;
  entry.dispatched = called;
}

const events = [
  { type: "thinking_text", text: "reason" },
  { type: "thinking_complete", thinking: "reason", signature: "signature" },
  { type: "stream_text", text: "first" },
  {
    type: "tool_use",
    toolId: "tool",
    toolName: "ReadFile",
    args: { file_path: "src/file.ts" },
  },
  { type: "stream_text", text: " second" },
  {
    type: "tool_result",
    toolId: "tool",
    toolName: "ReadFile",
    output: "result",
    isError: false,
    elapsed: 1.25,
  },
  { type: "steering_delivered", text: "next", inputId: "input" },
  { type: "stream_text", text: "third" },
  { type: "turn_complete" },
  { type: "usage", usage: { inputTokens: 12, outputTokens: 3 } },
  { type: "retry", reason: "retry", delay: 250 },
  { type: "compact", message: "compacted" },
  { type: "permission_request" },
  { type: "error", error: { message: "failure" } },
  { type: "loop_complete", stopReason: "end_turn" },
];
const s = server();
const messages = [];
s.broadcast = (message) => messages.push(message);
s.agentHandle = {
  permissionMode: "default",
  contextWindow: 200000,
  conv: new ConversationManager(),
  run: async function* () {
    yield* events;
  },
};
await s.streamAgentRun(s.agentHandle, null, Date.now());
for (const message of messages)
  if (message.type === "loop_complete") message.data.elapsed = "<elapsed>";
const result = {
  reference_commit: execFileSync(
    "git",
    ["-C", reference, "log", "-1", "--format=%H", "--", "src"],
    { encoding: "utf8" },
  ).trim(),
  source_hashes: Object.fromEntries(
    ["src/remote/server.ts", "src/ui/app.tsx"].map((path) => [
      path,
      createHash("sha256")
        .update(readFileSync(join(reference, path)))
        .digest("hex"),
    ]),
  ),
  protocol: cases,
  stream: { events, messages },
};
const output = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../internal/remote/testdata/host-reference.json",
);
mkdirSync(dirname(output), { recursive: true });
writeFileSync(output, JSON.stringify(result, null, 2) + "\n");
console.log(
  JSON.stringify({
    output,
    protocol: cases.length,
    streamMessages: messages.length,
  }),
);
