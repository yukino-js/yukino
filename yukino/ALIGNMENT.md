# Existing Module Alignment Ledger

The reference implementation is `~/github/yukino-code/apps/yukino`. M2 used the actual reference checkout at commit `8dbf96950d45fac1c7c68451478716898b35810d` (clean working tree). M1 recorded commit `3068413f121533ec0d7c4172fe680f4ce5d82fa1`. Earlier records retain their recorded baseline unless explicitly updated below; they have not all been revalidated against the M2 checkout.

The long-term target is file-by-file alignment of existing Go modules with the reference. At the user's request, the current delivery is a bounded milestone rather than a complete alignment pass. Missing files and behavioral gaps within those modules are in scope. Entirely absent modules, including a2a, images, goal, workflow, telemetry, vscode, update, and logger, are deferred. Implementation differences are acceptable only where TypeScript and Go require different language or runtime choices. Backward compatibility, deprecated APIs, and compatibility shims must not be retained.

Every previously tracked reference file retains its own record. A module-level counterpart identifies the area to review; it does not certify the entire Go package. Full alignment requires reading the complete reference file, comparing its implementation, and validating the resulting behavior. Partial progress and unresolved differences are recorded explicitly. Shared browser inputs follow the artifact delivery policy below.

Current inventory: **316 records**: 54 corrected, 15 reviewed, 47 partially aligned, 1 language-specific difference, 143 pending review, 1 deferred, 49 shared browser build inputs, 6 synchronized artifacts. **Full alignment is not complete.**

## Milestone M2 — scoped planning, delegation and conversation runtime (2026-11-01)

**Closed at the user's requested milestone cutoff.** This delivery advances existing modules only. a2a, images, goal, workflow, telemetry, vscode and the other previously excluded absent modules have not been implemented. The remaining existing-module alignment is handed to the next schedule; neither whole-module nor whole-application parity is claimed.

- Plan files belong to the permission checker instead of the process. EnterPlanMode invokes the built-in plan role; ExitPlanMode validates the plan and routes main-thread, subagent and teammate results correctly. UI approval keeps manual, YOLO and feedback behavior separate.
- Agent now uses current kind/role/fork/background/worktree fields, independent child TODO/background managers, scoped host approvals and subagent transcripts. /plan and /review run through that tool. Obsolete terminal teammate CLI, tmux/iTerm backends and verification/environment compatibility paths were removed.
- Teams support multiple canonical identities, TeamList, scoped messaging and awaited cancellation/cleanup. Manual plan review requires the leader, a matching request ID and review text; automatic approval resumes under current tool permissions. Ordinary mailbox messages survive a pending review, and notification XML is escaped.
- Conversation pairing repairs each assistant turn locally; session records preserve input metadata, reasoning and rich tool results, use timestamp, and recover from damaged compaction boundaries. UI/remote/print record user input through history. Clear/resume retain the conversation object used by delegated tools.
- Streaming execution preserves partial and opaque thinking, rejects malformed/truncated/filtered calls, reports per-turn usage and emits tool results as they finish. Runtime tool guidance follows current schemas; output caps apply to one provider request without changing shared client state.
- Remote connections use a random token and same-host origin checks. Address parsing and the scoped plan gate were advanced. The browser distribution is unchanged and still matches upstream byte for byte; backend protocol parity remains partial.

### M2 validation

- Passed `go test -race ./...` without exclusions after the user deleted `internal/mcp/mcp_test.go` / TestContext7MCP. This user change was preserved; the obsolete Makefile filter and dedicated integration target were removed.
- Passed `make check test`, including formatting validation, vet, the full race-enabled suite and the full ordinary suite without exclusions.
- Passed `go vet ./...` and `go build ./...`.
- Passed `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` and `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...`. These are compilation checks, not native-platform interaction tests.
- New regression coverage includes plan ownership and tool gates, team selection/message isolation/manual approval, mailbox corruption and XML escaping, call pairing, session metadata/boundary recovery/rewind/expiry, invalid tool calls, incomplete streams, per-turn usage and request output-budget isolation.
- Browser artifacts compared with `diff -qr` against the reference distribution. Formatting and `git diff --check` checked at handoff.
- No live provider, browser or terminal end-to-end interaction was exercised for M2. No commit or push was requested or created.

### Next schedule handoff

1. Finish teams and subagents: strict team-file validation, deterministic member/progress snapshots, subscription and disk-restoration APIs, retained-member pruning, spawn rollback, and teammate session/transcript/context-budget propagation. Finish task-manager persistence-error propagation and review the shared file-lock source completely.
2. Finish agent/permissions/worktree integration: steering delivery/removal, live worktree cwd and owned write roots, serialized approval dialogs for parallel calls, anonymous spill artifacts, transcript-aware compaction and rewind snapshots, remaining event fields and host cancellation/error paths.
3. Finish commands/history/session: Context output, equal-score completion order, integral-float YAML max_turns, strict activity checkpoint validation and I/O failures. Complete session error propagation/global listing/schema edge cases and user-Bash/rich-content rendering.
4. Read and compare the complete remote server, bootstrap and UI sources. Resolve bootstrap broadcasts to existing clients, pending-approval reconnect replay and cancellation/reset cleanup, busy input/steering queues, provider/permission controls and the remaining WebSocket protocol. Exercise actual browser and terminal flows.
5. Continue every remaining Pending review and Partially aligned row below. In particular, full LLM provider, compact, worktree, MCP, memory, skills, config, sandbox and UI comparisons remain open. Related edits in a pending row do not certify that reference file; complete unread files before changing its status.

The file ledger is the continuation checklist. Do not infer package parity from one corrected file or from passing tests. Historical M1 certifications retain their own baseline and limitations; revalidate them if the next schedule changes the reference checkout.

## Milestone M1 — existing tool execution (2026-11-01)

**Completed bounded delivery:** WebFetch and the TodoWrite core, together with their integration into the existing interactive, print, remote, and teammate entrypoints. This is not a claim of full module or application parity. Entirely absent modules remain excluded.

- TodoWrite replaces TaskCreate/Get/List/Update in private and shared boards. Writes are transactional, IDs are positive decimal strings, and the stored shape is `{nextId,tasks}`. Metadata, dependency links, claims, and assignment notifications follow the reference. The leader identity is `leader`; no old-name alias or snapshot migration is provided.
- WebFetch handles HTTP(S) text retrieval, redirects, conversion, limits, and caching. The native Go HTML converter can produce different Markdown formatting from Turndown; exact converted bytes are outside this milestone's parity claim.
- Delegated agents get separate private TODO boards; teammates get their team's board. Clear/resume switches the existing private board's store.

At the M1 cutoff, remaining work included leader-board subscriptions and aggregated TODO UI progress, teams/member lifecycle alignment, remote/WebSocket and bootstrap parity, EnterPlanMode, and ComputerUse. M2 implements EnterPlanMode and advances several of these areas; the M2 handoff supersedes this historical list. The shared store validates on access rather than in its Go constructor. Other pending ledger records remain pending; they were not sampled or marked complete by association.

Build and validation targets are available in `Makefile`: `build` (also the default), the six `win32`/`linux`/`darwin` arm64/x64 targets, `cross-compile`, `test`, `test-race`, `vet`, `fmt`, `fmt-check`, and `check`. Build outputs go to the ignored `dist/` directory; `BUILD_DIR`, `BUILD_FLAGS`, `LDFLAGS`, `BUILD_CGO_ENABLED`, `GO`, and `TEST_FLAGS` can be overridden. `check` runs formatting validation, vet, and race-enabled unit tests. The test targets now run the complete current suite without exclusions. The user deleted the external Context7 test during M2; its obsolete Makefile filter and dedicated target have been removed.

## Status Definitions

| Status | Meaning |
| --- | --- |
| Pending review | The complete source-to-implementation comparison and validation remain outstanding. |
| Reviewed | The recorded comparison is complete to the extent described in the notes; any stated integration work remains open. |
| Corrected | Identified differences have been addressed and validated to the extent recorded. |
| Partially aligned | Some behavior has been reviewed or corrected; the notes identify remaining work. |
| Language-specific difference | An explicit TypeScript/Go language or runtime difference is documented and accepted. |
| Deferred: missing module | Work depends on an entirely absent module excluded from this phase. |
| Shared browser build | An upstream browser source or build input is reused through the TypeScript build output, with no separate Go port required. |
| Synchronized artifact | A generated browser file is copied unchanged from the reference distribution and verified byte for byte. |

## Browser Bundle Delivery

The TypeScript project owns the browser source and its build. `internal/remote/browser/dist` is a direct copy of `~/github/yukino-code/apps/yukino/src/remote/browser/dist`. Preserve the complete output, including `index.html`, JavaScript chunks, and CSS. Browser source entries below describe inputs to this shared build and are excluded from the Go porting backlog. Artifact reuse does not establish parity for the Go remote backend or its WebSocket protocol.

After building the browser in the TypeScript project with `pnpm build:browser`, synchronize its output from this repository:

```sh
make sync-browser-dist
```

The target checks for a nonempty `index.html`, copies the complete distribution with `rsync --delete`, and removes obsolete destination artifacts. A different source directory can be supplied explicitly:

```sh
make sync-browser-dist BROWSER_DIST_SOURCE=/path/to/browser/dist
```

`internal/remote/web.go` embeds the distribution in the Go binary and serves the requested asset with the reference MIME type. Unmatched paths fall back to `index.html`, as in the TypeScript server. The bundle is available independently of the process working directory. Rebuild the Go binary after synchronizing artifacts.

All six distribution files currently match the reference byte for byte. `internal/remote/web_test.go` verifies every embedded artifact, the entry page, SPA fallback, working-directory independence, and HEAD/range responses.

## Validation

The existing alignment work has passed `go test -race ./...`, `go vet ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, and `GOOS=windows GOARCH=amd64 go build ./...`. An external Context7 test previously timed out; an isolated rerun and the subsequent full race-enabled suite passed. Cross-compilation does not establish runtime integration on Windows or Linux.

The browser-serving fix has also passed the full race-enabled suite and `go vet ./...`. The synchronization target was verified to copy every artifact, remove obsolete files, support paths containing spaces, and preserve the destination when the source entry is missing. The synchronized distribution was compared file by file with the TypeScript output.

## M1 Validation

- Passed `go test -race ./... -skip '^TestContext7MCP$'` for all local packages, including HTTP fixtures, transaction rollback, session persistence, delegated board isolation, and simultaneous shared-board writers.
- Passed `go vet ./...` and `make check test` (formatting validation, vet, race-enabled unit tests, and ordinary unit tests).
- Passed `make build` and `make -j2 cross-compile`; inspected all seven binaries with `go version -m` to verify OS, architecture, and CGO settings.
- Passed `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` and `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...`. The initial Linux amd64 attempt with CGO enabled failed in the host C assembler; disabling CGO permits the cross-build. Cross-compilation does not establish native Windows runtime parity.
- Historical M1 result: the unfiltered suite encountered `TestContext7MCP` timing out while connecting to the external server. M1 excluded that one test. The user subsequently deleted it during M2; see the unfiltered M2 validation.
- Browser distribution files remain unchanged and still compare byte for byte with the reference distribution. `git diff --check` passed.

## File-by-File Ledger

| Reference file | Go counterpart or shared artifact | Status | Differences and validation |
| --- | --- | --- | --- |
| `src/agent/constants.ts` | `internal/agent` | Reviewed | M2: Full source read. Output ceiling, recovery count, and per-result spill threshold checked. Worktree reminder consumption is still part of the partial agent implementation. |
| `src/agent/events.ts` | `internal/agent` | Partially aligned | M2: Full source read. Thinking completion, stop reasons, per-turn cache-aware usage, and rich result fields added. Steering/tool-output and the complete host event contract remain outstanding. |
| `src/agent/index.ts` | `internal/agent/agent.go`, `internal/agent/run.go` | Partially aligned | M2: Full source read. Scoped planning, active/deferred tool guidance, partial-stream preservation, completion validation, bounded retries, cancellation, argument-error gating, filtered-tool rejection, MCP target checks, hook notifications, and per-turn usage corrected. Steering queues, live worktree cwd, anonymous spill sessions, transcript-aware compaction/snapshots, and complete host/lifecycle integration remain open. Permission-dialog serialization for concurrent calls still requires integration coverage. See agent/alignment_test.go. |
| `src/agent/prompts.ts` | `internal/agent` | Partially aligned | M2: Full source read. Retry and denied-tool wording aligned where consumed. Working-directory reminders and their worktree integration remain outstanding; goal behavior is excluded. |
| `src/agent/streaming-executor.ts` | `internal/agent/streaming_executor.go`, `internal/agent/agent.go` | Partially aligned | M2: Full source read. Results are emitted as calls finish, while persistence retains call order. Parse errors and cancellation prevent execution; execution panics become tool errors. Go still combines permission/hook preparation with execution rather than the reference executor boundary. |
| `src/bootstrap/exit-cleanup.ts` | `internal/tools/exit_cleanup.go`, `cmd/yukino/main.go` | Corrected | Registered synchronous, idempotent cleanup and integrated it with normal, signal-driven, and error exits. |
| `src/bootstrap/index.ts` | `cmd/yukino`, `internal/ui`, `internal/remote` | Pending review |  |
| `src/bootstrap/interaction-summary.ts` | `cmd/yukino`, `internal/ui`, `internal/remote` | Pending review |  |
| `src/bootstrap/sandbox.ts` | `cmd/yukino`, `internal/ui`, `internal/remote` | Pending review |  |
| `src/bootstrap/tool-registry.ts` | `internal/tools/tool.go`, `internal/ui/agent_setup.go`, `cmd/yukino/print.go`, `internal/remote/server.go` | Partially aligned | M2: Existing tools now include EnterPlanMode, scoped ExitPlanMode, TeamList, and the revised Agent runtime across UI/remote/print. Legacy terminal teammate entrypoint/backends removed. Full bootstrap source comparison, ordering, ComputerUse and lifecycle parity remain open; Goal is excluded. |
| `src/cli.ts` | `cmd/yukino` | Pending review |  |
| `src/commands/commands.ts` | `internal/commands` | Partially aligned | M2: Full source read. Registry ordering/completion and agent-role commands updated. Context command output and the complete UI/remote command handling remain open. |
| `src/commands/constants.ts` | `internal/commands` | Reviewed | M2: Full source read; command constants compared. No claim of complete host command parity. |
| `src/commands/index.ts` | `internal/commands` | Reviewed | M2: Full source read. Go package exports replace the TypeScript barrel. |
| `src/commands/loader.ts` | `internal/commands` | Corrected | M2: Full source compared. Raw command bodies/BOM, atomic string metadata validation, argument slice length zero, argument parsing and Unicode command whitespace corrected; loader_test.go covers the changed boundaries. |
| `src/commands/prompts.ts` | `internal/commands` | Reviewed | M2: Full source read and compared with the command prompt definitions. |
| `src/commands/subagent.ts` | `internal/commands/subagent.go`, `internal/ui/send_message.go`, `internal/remote/server.go` | Partially aligned | M2: Full source read. /plan and /review use the Agent tool with the correct role/fork, scoped approval forwarding, input/session metadata, result persistence, and synthetic events. Complete event/error and host UI integration still require comparison. |
| `src/commands/usage-tracker.ts` | `internal/commands` | Partially aligned | M2: Full source read. Explicit nonpositive limits return no results. Equal-score ordering still depends on Go map iteration and must be made deterministic. |
| `src/compact/compact.ts` | `internal/compact` | Pending review |  |
| `src/compact/constants.ts` | `internal/compact` | Pending review |  |
| `src/compact/index.ts` | `internal/compact` | Pending review |  |
| `src/compact/prompts.ts` | `internal/compact` | Pending review |  |
| `src/compact/recovery.ts` | `internal/compact` | Pending review |  |
| `src/config/constants.ts` | `internal/config` | Pending review |  |
| `src/config/index.ts` | `internal/config` | Pending review |  |
| `src/config/provider-config.ts` | `internal/config` | Pending review |  |
| `src/config/provider-login.ts` | `internal/config` | Pending review |  |
| `src/conversation/at-expand.ts` | `internal/conversation` | Pending review |  |
| `src/conversation/constants.ts` | `internal/conversation` | Reviewed | M2: Full source read. Tool-interruption and rejection strings checked; reminder handling is recorded separately. |
| `src/conversation/index.ts` | `internal/conversation/conversation.go` | Partially aligned | M2: Full source read. Forks deep-copy nested messages; reminder escaping and metadata fields added. User-input/bash host coverage and all structured-content/provider paths remain incomplete. |
| `src/conversation/pairing.ts` | `internal/conversation/pairing.go` | Corrected | M2: Full source compared. Results are grouped immediately after their assistant turn; missing results are filled there, orphan/duplicate/late results are dropped, and extra text is preserved without mutating input. Covered by pairing_test.go and alignment_test.go. |
| `src/conversation/prompts.ts` | `internal/conversation/conversation.go` | Partially aligned | M2: Full source read. Project/memory/skill wording, UTC date and nested reminder escaping corrected. ECMAScript/Go whitespace edge cases and user-Bash host integration still need the final comparison. |
| `src/conversation/user-bash.ts` | `internal/conversation` | Pending review |  |
| `src/file-history/constants.ts` | `internal/file_history` | Pending review |  |
| `src/file-history/index.ts` | `internal/file_history` | Pending review |  |
| `src/history/constants.ts` | `internal/history/history.go` | Reviewed | Values, filenames, and messages checked individually against the reference. |
| `src/history/index.ts` | `internal/history/history.go`, `internal/history/signature_*.go` | Partially aligned | Full source reviewed. Platform-specific cache identity and ctime handling corrected. Strict checkpoint validation and error propagation remain outstanding. |
| `src/hooks/constants.ts` | `internal/hooks` | Pending review |  |
| `src/hooks/index.ts` | `internal/hooks` | Pending review |  |
| `src/index.ts` | `cmd/yukino` | Pending review |  |
| `src/llm/anthropic.ts` | `internal/llm` | Pending review | M2: Complete reference-file comparison remains outstanding. Related Go integration now honors request-scoped output caps and replays opaque thinking; this limited change does not certify provider parity. |
| `src/llm/client.ts` | `internal/llm` | Pending review | M2: Complete reference-file comparison remains outstanding. Request output override uses context instead of changing the shared client; old max-output setter APIs still need review/removal with provider alignment. |
| `src/llm/constants.ts` | `internal/llm` | Pending review |  |
| `src/llm/errors.ts` | `internal/llm` | Pending review |  |
| `src/llm/events.ts` | `internal/llm` | Partially aligned | M2: Full source read. Provider item ID is represented and parse errors/redacted thinking reach the agent. Provider population and all downstream event consumers remain open. |
| `src/llm/index.ts` | `internal/llm` | Pending review |  |
| `src/llm/model-discovery.ts` | `internal/llm` | Pending review |  |
| `src/llm/openai.ts` | `internal/llm` | Pending review | M2: Complete reference-file comparison remains outstanding. Responses and Chat Completions honor request-scoped output caps; SDK boundary tests check that overrides do not leak into later calls. Structured content, provider item IDs and the broader provider contract remain open. |
| `src/llm/retry.ts` | `internal/llm` | Reviewed | M2: Full source read. Existing LLMRetryDelay is now used by the agent, bounded to retries before visible output. Broader provider retry layering is not certified. |
| `src/main.tsx` | `cmd/yukino` | Pending review |  |
| `src/mcp/apps.ts` | `internal/mcp` | Pending review |  |
| `src/mcp/client.ts` | `internal/mcp` | Pending review |  |
| `src/mcp/constants.ts` | `internal/mcp/strategy.go` and related files | Partially aligned | Full source reviewed. Strategy constants checked; MCP Apps constants still require alignment. |
| `src/mcp/index.ts` | `internal/mcp` | Pending review |  |
| `src/mcp/instructions.ts` | `internal/mcp` | Pending review |  |
| `src/mcp/manager.ts` | `internal/mcp` | Pending review |  |
| `src/mcp/prompts.ts` | `internal/mcp` | Pending review |  |
| `src/mcp/strategy.ts` | `internal/mcp/strategy.go` | Corrected | Protocol gating and the mcp_loading setting are wired in; the legacy environment override was removed. Validated by strategy_test.go. |
| `src/mcp/tool-wrapper.ts` | `internal/mcp` | Pending review |  |
| `src/memory/background.ts` | `internal/memory` | Pending review |  |
| `src/memory/consolidation.ts` | `internal/memory/consolidation/consolidation.go` | Partially aligned | Full source reviewed. Permissions and an independent tool pool implemented. Success timestamps, the running lock, and shutdown cancellation still require revision. |
| `src/memory/constants.ts` | `internal/memory` | Pending review |  |
| `src/memory/extractor.ts` | `internal/memory/extractor/extractor.go` | Partially aligned | Full source reviewed. Permissions and an independent tool pool implemented. Summary-only input, text fallback, and shutdown cancellation still require revision. |
| `src/memory/index.ts` | `internal/memory` | Pending review |  |
| `src/memory/instructions.ts` | `internal/memory` | Pending review |  |
| `src/memory/manager.ts` | `internal/memory` | Pending review |  |
| `src/memory/memory-age.ts` | `internal/memory` | Pending review |  |
| `src/memory/permissions.ts` | `internal/memory/permissions.go` | Corrected | Removed permission bypasses. Markdown writes are restricted to memory roots, scan scope is constrained, and explicit ask/deny rules reject access. Validated by permissions_test.go. |
| `src/memory/prompts.ts` | `internal/memory` | Pending review |  |
| `src/memory/written-paths.ts` | `internal/memory` | Pending review |  |
| `src/permissions/constants.ts` | `internal/permissions/constants.go`, `internal/tools/safe_command.go` | Reviewed | Existing constants and the allowlist checked individually. Constants associated with deferred modules do not enable those features. |
| `src/permissions/index.ts` | `internal/permissions/permissions.go`, `internal/permissions/mode.go` | Partially aligned | M2: Previously reviewed full source; M2 adds synchronized owner-scoped plan paths and child path isolation. OwnedWorktrees, isolated roots, live cwd and remaining lifecycle behavior are still open. |
| `src/permissions/request.ts` | `internal/agent/agent.go`, `internal/subagent/agent_tool.go`, `internal/commands/subagent.go` | Partially aligned | M2: Delegated approvals now forward to the owning host; remembered patterns are captured before waiting and cancellation prevents execution. Full permission-dialog/request serialization and host coverage remain open. |
| `src/permissions/shell.ts` | `internal/permissions/shell.go` | Reviewed | Compared line by line. Shell segmentation and Unicode whitespace semantics corrected. |
| `src/plan-file/constants.ts` | `internal/plan_file` | Corrected | M2: Full source compared; storage directory, extension and diagnostic aligned. |
| `src/plan-file/index.ts` | `internal/plan_file/plan_file.go` | Corrected | M2: Full source compared. Process-global ownership removed; each checker owns its plan path. Private exclusive creation, storage-boundary checks, save/load/reset, and parent/child isolation are covered by plan_file_test.go. Go uses locked accessor methods and returned errors. |
| `src/print-mode.ts` | `cmd/yukino` | Pending review |  |
| `src/prompt/builder.ts` | `internal/prompt/builder.go` | Corrected | M2: Full source compared. Insertion order with content deduplication replaces priority sorting; UTC dates, Node architecture names, bounded shell-free Git probes and branch length cap aligned. |
| `src/prompt/constants.ts` | `internal/prompt` | Reviewed | M2: Full source read; guidance markers, reminder interval, probe timeout and branch cap checked. |
| `src/prompt/coordinator.ts` | `internal/prompt/coordinator.go` | Corrected | M2: Full source compared. Current coordinator text replaces obsolete mandatory verification/commit instructions; sparse reminders retained. Covered through team coordinator tests. |
| `src/prompt/index.ts` | `internal/prompt` | Reviewed | M2: Full source read. Go package exports replace the namespace barrel. |
| `src/prompt/plan-mode.ts` | `internal/prompt/plan_mode.go`, `internal/prompt/plan_prompts.go` | Corrected | M2: Full source compared. Reminders use actual callable tools, coordinator/delegated/read-only role and manual/automatic leader approval policy. |
| `src/prompt/prompts.ts` | `internal/prompt` | Partially aligned | M2: Full source read. Base, coordinator, plan, teammate and active/deferred tool text synchronized. Goal/workflow consumers remain excluded; complete user-facing integration has not been certified. |
| `src/prompt/sections.ts` | `internal/prompt/sections.go` | Corrected | M2: Full source compared. All base sections and environment rendering follow the reference insertion order. |
| `src/prompt/tools.ts` | `internal/prompt/tools.go`, `internal/agent/run.go` | Corrected | M2: Full source compared for existing callable modules. Tool guidance derives from visible schemas; deferred discovery requires ToolSearch and dispatch-mode McpCall. Goal/workflow branches are excluded with their missing modules. |
| `src/recover.ts` | `cmd/yukino` | Pending review |  |
| `src/remote/address.ts` | `internal/remote/access.go` | Corrected | M2: Full source compared. Bare ports, default/loopback binding, IPv6 and port/address rejection covered by access_test.go. |
| `src/remote/browser/dist/index.html` | `internal/remote/browser/dist/index.html` | Synchronized artifact | Byte-for-byte identical to the corresponding TypeScript build artifact; embedded and served without modification. |
| `src/remote/browser/dist/static/chunk-OR5FHECQ.js` | `internal/remote/browser/dist/static/chunk-OR5FHECQ.js` | Synchronized artifact | Byte-for-byte identical to the corresponding TypeScript build artifact; embedded and served without modification. |
| `src/remote/browser/dist/static/highlight-ZDY7K2CR.js` | `internal/remote/browser/dist/static/highlight-ZDY7K2CR.js` | Synchronized artifact | Byte-for-byte identical to the corresponding TypeScript build artifact; embedded and served without modification. |
| `src/remote/browser/dist/static/index.css` | `internal/remote/browser/dist/static/index.css` | Synchronized artifact | Byte-for-byte identical to the corresponding TypeScript build artifact; embedded and served without modification. |
| `src/remote/browser/dist/static/index.js` | `internal/remote/browser/dist/static/index.js` | Synchronized artifact | Byte-for-byte identical to the corresponding TypeScript build artifact; embedded and served without modification. |
| `src/remote/browser/dist/static/webapi-HZESKPJE.js` | `internal/remote/browser/dist/static/webapi-HZESKPJE.js` | Synchronized artifact | Byte-for-byte identical to the corresponding TypeScript build artifact; embedded and served without modification. |
| `src/remote/browser/index.html` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/app.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/activity-panel.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/ask-user-dialog.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/assistant-message.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/collapsible.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/diff-lines.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/done-indicator.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/error-message.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/input-area.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/markdown.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/mcp-app.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/message-list.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/modal.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/model-picker.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/permission-dialog.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/picker-list.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/plan-approval-dialog.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/provider-dialog.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/rewind-dialog.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/session-picker.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/slash-menu.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/status-bar.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/streaming-cursor.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/system-message.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/thinking-block.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/thinking-picker.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/todo-progress.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/tool-block.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/user-message.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/components/welcome.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/constants.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/hooks/offline-queue.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/hooks/use-auto-scroll.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/hooks/use-chat.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/hooks/use-scroll-follow.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/hooks/use-theme.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/hooks/use-web-socket.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/index.tsx` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/lib/format.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/lib/highlight.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/lib/markdown.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/lib/mcp-app-context.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/lib/mcp-app-requests.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/lib/mcp-app-sandbox.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/main.css` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/shims.d.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/src/types.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/browser/tsup.config.ts` | `internal/remote/browser/dist` | Shared browser build | Upstream browser build input; delivered through the synchronized bundle. See Browser Bundle Delivery. |
| `src/remote/constants.ts` | `internal/remote/web.go`, `internal/remote/server.go` | Partially aligned | Static-asset MIME types match the reference. Other remote constants and their consumers remain pending review. |
| `src/remote/index.ts` | `internal/remote` | Pending review |  |
| `src/remote/log.ts` | `internal/remote` | Pending review |  |
| `src/remote/mcp-apps.ts` | `internal/remote` | Pending review |  |
| `src/remote/server.ts` | `internal/remote/server.go`, `internal/remote/plan.go` | Partially aligned | M2: Complete 4K-line reference comparison remains outstanding. Scoped plan gates, run locking, role commands, user-input metadata and in-place conversation reset added. Reconnect replay, unicast bootstrap, pending-gate cleanup, busy input/steering, provider controls and the complete protocol remain open. |
| `src/remote/session-state.ts` | `internal/remote` | Pending review |  |
| `src/sandbox/bwrap.ts` | `internal/sandbox/bwrap.go` | Corrected | Structured argv, working-directory resolution, and --die-with-parent aligned. Validated by sandbox_test.go. |
| `src/sandbox/constants.ts` | `internal/sandbox/seatbelt.go` | Corrected | Fixed executable paths and invalid-path-character errors aligned. Validated by sandbox_test.go. |
| `src/sandbox/index.ts` | `internal/sandbox/sandbox.go` | Corrected | Structured Prepare arguments aligned. Go type assertions express optional cleanup support. Validated by go test ./internal/sandbox. |
| `src/sandbox/seatbelt.ts` | `internal/sandbox/seatbelt.go` | Corrected | Path aliases, profile escaping, /dev/null handling, and deny rules for literal paths and subpaths aligned. Validated by sandbox_test.go. |
| `src/session/constants.ts` | `internal/session` | Reviewed | M2: Full source read. File/expiry/preview values and input-session mismatch checked; rewind temporary files and artifact cleanup now implemented. |
| `src/session/index.ts` | `internal/session/session.go`, `internal/session/messages.go` | Partially aligned | M2: Full source read. Uses timestamp (no ts migration), preserves thinking/redacted blocks, provider IDs, rich results and input/bash metadata; validates records, skips damaged boundaries, unifies resume reconstruction, provides locked rewind line operations, registers projects and removes expired artifacts. Tests cover round trips, wrong-project input, malformed boundaries and rewind/expiry. Complete I/O error propagation, fractional timestamp schema fidelity, global session listing and UI user-Bash rendering remain open. |
| `src/skills/catalog.ts` | `internal/skills` | Pending review |  |
| `src/skills/constants.ts` | `internal/skills` | Pending review |  |
| `src/skills/executor.ts` | `internal/skills` | Pending review |  |
| `src/skills/index.ts` | `internal/skills` | Pending review |  |
| `src/skills/install-skill-tool.ts` | `internal/skills` | Pending review |  |
| `src/skills/load-skill-tool.ts` | `internal/skills/load_skill_tool.go` | Partially aligned | Full source reviewed. Delegated skills no longer trigger parent-host side effects. Output text, errors, and executor behavior still require alignment. |
| `src/skills/prompts.ts` | `internal/skills` | Pending review |  |
| `src/storage/constants.ts` | `internal/storage` | Pending review |  |
| `src/storage/index.ts` | `internal/storage` | Pending review |  |
| `src/storage/private.ts` | `internal/storage` | Pending review |  |
| `src/subagent/agent-tool.ts` | `internal/subagent/agent_tool.go` | Partially aligned | M2: Full source read. kind/role/fork/background/worktree semantics, fresh provider/system prompt selection, independent TODO/background managers, transcript metadata, host approvals and scoped teammate tools implemented. Teammate transcript/session/context-budget propagation, runtime progress forwarding and persistence-error handling remain open. |
| `src/subagent/constants.ts` | `internal/subagent` | Reviewed | M2: Full source read; current names/defaults/error record type checked. No default model-turn cap retained. |
| `src/subagent/definition.ts` | `internal/subagent/definition.go`, `internal/subagent/subagent.go` | Partially aligned | M2: Full source read. Current YAML schema/body and built-in roles replace obsolete name/isolation/mode semantics. Validation and built-in defaults tested. Integral floating YAML values for max_turns remain a schema edge to resolve. |
| `src/subagent/index.ts` | `internal/subagent` | Reviewed | M2: Full source read. Go package exports replace the TypeScript barrel. |
| `src/subagent/loader.ts` | `internal/subagent/loader.go` | Corrected | M2: Full source compared. Only user definitions load, built-in insertion order is stable, user overrides retain their slot, and reload drops removed definitions. Covered by loader_test.go. |
| `src/subagent/plan.ts` | `internal/subagent/agent_tool.go`, `internal/tools/enter_plan_mode.go` | Corrected | M2: Full source compared. Built-in foreground plan runner bypasses user role overrides, forks the parent and writes/returns the scoped plan; covered by plan tool tests. |
| `src/subagent/prompts.ts` | `internal/subagent/prompts.go`, `internal/subagent/subagent.go` | Corrected | M2: Full source compared. Agent descriptions and current role instructions replace old verification/environment instructions. |
| `src/subagent/spawn.ts` | `internal/subagent/agent_tool.go`, `internal/teams/spawn.go`, `internal/teams/in_process.go` | Partially aligned | M2: Full source read. In-process spawn receives checker, role, context snapshot and work directory. Complete teammate session/transcript, budgets and cleanup/rollback remain open. |
| `src/subagent/task-manager.ts` | `internal/subagent/task_manager.go` | Partially aligned | Task lifecycle, retention after cancellation, waits, notifications, retention limits, and session restoration corrected. Complete propagation of persistence failures remains outstanding. Validated by task_manager_test.go. |
| `src/subagent/task-store.ts` | `internal/subagent/task_store.go` | Corrected | Private, atomic JSON records, ID validation, and restoration ordering aligned. Validated by task_manager_test.go. |
| `src/subagent/tool-filter.ts` | `internal/subagent/tool_filter.go`, `internal/subagent/agent_tool.go` | Corrected | M2: Full source compared; M1 scoped registries/private TODO retained. M2 removes double cloning during teammate filtering and keeps background TaskStop scoped. Borrowed-tool lifetime and board isolation tests pass. |
| `src/teams/constants.ts` | `internal/teams` | Partially aligned | M2: Full source read. Leader identity, in-process protocol and approval/shutdown constants consumed by the revised runtime. Retention and full lifecycle consumers remain outstanding. |
| `src/teams/coordinator.ts` | `internal/teams/coordinator.go` | Corrected | M2: Full source compared. Current coordinator allowlist, including TeamCreate/TeamList/TodoWrite, and live mode binding aligned for existing modules; absent Goal/Workflow are excluded. |
| `src/teams/file-lock.ts` | `internal/teams` | Pending review |  |
| `src/teams/file-mailbox.ts` | `internal/teams/file_mailbox.go` | Partially aligned | M2: Full source read. Private atomic files, strict message fields, member-path validation, corruption preservation, selective atomic receive/requeue and pruning implemented. Shared file-lock source review and full no-op write/error semantics remain outstanding. |
| `src/teams/index.ts` | `internal/teams/teams.go`, `internal/teams/runner.go`, `internal/teams/in_process.go`, `internal/teams/plan.go` | Partially aligned | M2: Full source read. Multiple canonical team identities, exclusive creation, hydration to inactive members, scoped permission modes, awaited stop/delete/close and task release implemented. Disk restoration/subscriptions, terminated-member retention, rollback and complete error propagation still need work. |
| `src/teams/progress.ts` | `internal/teams/progress.go` | Partially aligned | M2: Full source read. Usage accumulates once per turn; active tool/turn counters and last-message state added. Activity fallbacks/UTF-16 clipping, serializable snapshots and complete UI consumers remain open. |
| `src/teams/prompts.ts` | `internal/teams/prompts.go`, `internal/prompt/teammate.go` | Corrected | M2: Full source compared. Team tools, automatic-plan approval and shutdown wording synchronized; legacy terminal-backend wording removed. |
| `src/teams/protocol.ts` | `internal/teams/protocol.go`, `internal/teams/runner.go`, `internal/teams/plan.go` | Corrected | M2: Full source compared. Shutdown and plan review use typed messages with request IDs and leader authority; old text-prefix shutdown inference removed. Protocol/alignment tests cover stale, duplicate and forged responses. |
| `src/teams/registry.ts` | `internal/teams` | Pending review |  |
| `src/teams/shared-task.ts` | `internal/teams/shared_task.go`, `internal/todo/write.go`, `internal/todo/store.go` | Partially aligned | M1: full source compared. Atomic incremental writes, file locking with reload, rollback, createdBy, owner release, dependency validation, notifications, and corrupt-board preservation implemented. Separate instances and child processes validated by shared_task_test.go. Go reports unreadable state on first access rather than during construction; existing file-lock implementation has not received a new full source comparison. |
| `src/teams/task-stop.ts` | `internal/teams/task_stop.go` | Corrected | M2: Full source compared. Team/member disambiguation, scoped task IDs, cancellation waits and cleanup implemented. Task/tool and alignment tests pass. |
| `src/teams/task-tools.ts` | `internal/teams/task_tools.go` | Partially aligned | M2: M1 board semantics retained; M2 resolves the sender/team scope and rejects ambiguous names across teams. Leader subscriptions, aggregated UI progress and complete lifecycle integration remain open. |
| `src/teams/team-file.ts` | `internal/teams/team_file.go` | Partially aligned | M2: Full source read. Uses leaderAgentId and permissionMode without backend fields; private atomic writes retained. Strict reference schema validation, stable member ordering and persistence failure propagation remain open. |
| `src/teams/tools.ts` | `internal/teams/tools.go` | Corrected | M2: Full source compared. Optional team creation, TeamList, explicit/ambiguous team selection, scoped sender/broadcast, manual leader plan reviews and teardown replace old single-team/terminal interfaces. Covered by teams/alignment_test.go and existing tool tests. |
| `src/theme/constants.ts` | `internal/ui/theme.go` | Pending review |  |
| `src/theme/index.ts` | `internal/ui/theme.go` | Pending review |  |
| `src/theme/syntax.ts` | `internal/ui/theme.go` | Pending review |  |
| `src/todo/constants.ts` | `internal/todo/write.go`, `internal/todo/store.go`, `internal/todo/todo.go` | Corrected | M1: full source compared. 200-item limits, task filename, private board prefix, and required-subject error aligned. |
| `src/todo/dependencies.ts` | `internal/todo/write.go` | Corrected | M1: full source compared. Reciprocal links, unique IDs, unknown/self dependency rejection, cycle detection, and unresolved blockers validated by todo/todo_test.go. |
| `src/todo/index.ts` | `internal/todo/todo.go` | Corrected | M1: full source compared. Store switching, in-memory boards, immutable snapshots, listeners, incremental writes, and publish-after-persistence semantics implemented. UI clear/resume now rebind the existing board rather than leave tools pointing at stale state. |
| `src/todo/progress.ts` | `internal/todo/todo.go` | Corrected | M1: full source compared. DONE completed/total includes every task in the total and counts only completed tasks as done. Rendering remains part of the pending UI work. |
| `src/todo/prompts.ts` | `internal/todo/prompts.go`, `internal/todo/tools.go` | Corrected | M1: full source compared. TodoWrite description and all model-facing field descriptions synchronized; obsolete task-tool descriptions removed. |
| `src/todo/store.ts` | `internal/todo/store.go` | Corrected | M1: full source compared. Decimal IDs, strict nextId snapshots, private atomic persistence, monotonic IDs after deletion, corrupt-state rejection, and session isolation validated. Legacy task-array snapshots are rejected without migration. |
| `src/todo/tools.ts` | `internal/todo/tools.go` | Corrected | M1: full source compared. Strict TodoWrite schema, read category, cancellation before mutation, boardId/todos/progress output, and private board scoping implemented. Legacy TaskCreate/Get/List/Update removed. |
| `src/todo/write.ts` | `internal/todo/write.go` | Corrected | M1: full source compared. Incremental fields, explicit deletion, metadata merge/null removal, reciprocal dependency replacement, pre-call IDs, final-batch validation, teammate claims, owner rules, single active task, and rollback implemented and covered by private/shared board tests. |
| `src/tool-result/constants.ts` | `internal/tool_result` | Pending review |  |
| `src/tool-result/index.ts` | `internal/tool_result` | Pending review |  |
| `src/tool-result/prompts.ts` | `internal/tool_result` | Pending review |  |
| `src/tools/ask-user.ts` | `internal/tools` | Pending review |  |
| `src/tools/atomic-write.ts` | `internal/tools/atomic_write.go` | Corrected | Same-directory temporary files, fsync and rename, symlink handling, and permission preservation aligned. Validated by file_tools_alignment_test.go. |
| `src/tools/bash.ts` | `internal/tools/bash.go`, `internal/tools/shell_background.go` | Partially aligned | Cancellation, background execution, timeouts, persisted output, and sandboxing implemented. Isolated-worktree write roots and temporary-directory integration remain outstanding. |
| `src/tools/computer-use.ts` | `internal/tools` | Pending review |  |
| `src/tools/constants.ts` | `internal/tools/constants.go` and related files | Partially aligned | Full source reviewed. File, search, and shell budgets and error messages implemented. M1 adds WebFetch budgets and errors; ComputerUse constants and their consumers remain outstanding. |
| `src/tools/diff.ts` | `internal/tools/diff.go` | Corrected | Separate edit regions, accurate addition/deletion counts, new-file line numbers, and truncation aligned. Validated by diff_test.go and file_tools_alignment_test.go. |
| `src/tools/edit-file-diagnostics.ts` | `internal/tools/edit_file_diagnostics.go` | Corrected | Whitespace candidates and Unicode difference positions aligned. Go uses rune-based character indexing. Validated by file_tools_alignment_test.go. |
| `src/tools/edit-file.ts` | `internal/tools/edit_file.go` | Corrected | Only the batch edits interface is accepted. Original-text matching, atomic failure, and preservation of CRLF and stray carriage returns aligned. Validated by file_tools_alignment_test.go. |
| `src/tools/enter-plan-mode.ts` | `internal/tools/enter_plan_mode.go` | Corrected | M2: Full source compared. Main-thread-only builtin plan delegation, empty/interrupted/error checks and owner-scoped saving implemented. Covered by plan_mode_test.go. |
| `src/tools/enter-worktree.ts` | `internal/tools` | Pending review |  |
| `src/tools/execution-coordinator.ts` | `internal/tools/execution_coordinator.go` | Partially aligned | Workspace write serialization and permission rechecks after waiting implemented. Runtime working-directory changes for worktrees remain outstanding. |
| `src/tools/exit-code-hints.ts` | `internal/tools/exit_code_hints.go` | Corrected | Quoted command segments, skipped variable assignments, and command/signal exit hints aligned. Validated by shell_alignment_test.go. |
| `src/tools/exit-plan-mode.ts` | `internal/tools/exit_plan_mode.go` | Corrected | M2: Full source compared. Requires active mode and a nonempty plan; delegated output and teammate approval callbacks are scoped, and only successful planning pauses the agent. Covered by plan_mode_test.go. |
| `src/tools/exit-worktree.ts` | `internal/tools` | Pending review |  |
| `src/tools/file-mutation-queue.ts` | `internal/tools/execution_coordinator.go` | Corrected | Mutations are serialized by canonical path; waiting can be cancelled. Validated by concurrency regression tests. |
| `src/tools/file-state-cache.ts` | `internal/tools/file_state_cache.go` | Corrected | Canonical keys, rejection after deletion or backward timestamp changes, and has/delete/clear operations aligned. Validated by file_tools_alignment_test.go. |
| `src/tools/glob.ts` | `internal/tools/glob.go`, `internal/tools/search_output.go` | Partially aligned | Working-directory handling, brace expansion, globstar, dotfiles, limits, and sorting implemented. The minimatch extglob syntax remains unsupported. |
| `src/tools/goal.ts` | — | Deferred: missing module | Depends on the goal module, which is explicitly outside the scope of this phase. |
| `src/tools/grep.ts` | `internal/tools/grep.go` | Corrected | JavaScript Unicode-regex conversion, timeouts, file/directory handling, binary files, symlinks, and traversal/output budgets aligned. Validated by search_alignment_test.go. |
| `src/tools/index.ts` | `internal/tools` | Partially aligned | Full source reviewed. Go package exports replace the TypeScript barrel; some tools remain unported. |
| `src/tools/is-diff-tool.ts` | `internal/ui/tool_display.go` | Reviewed | Source reviewed. Only EditFile uses diff rendering; integration still requires validation during the complete UI review. |
| `src/tools/mcp-call.ts` | `internal/tools/mcp_call.go` | Corrected | Calls are restricted to MCP tools on the same server. Argument-object validation and coercion aligned. Validated by mcp_call_test.go and registry_alignment_test.go. |
| `src/tools/powershell.ts` | `internal/tools/powershell.go`, `internal/tools/shell_background.go` | Partially aligned | Shares the shell lifecycle and includes Windows taskkill arguments. PowerShell and Windows integration have not been exercised on this host. |
| `src/tools/prompts.ts` | `internal/tools/descriptions.go` and related files | Partially aligned | Full source reviewed. Descriptions for the six file/search tools and shell tools synchronized. M1 also synchronizes WebFetch and TodoWrite descriptions. Descriptions for the remaining tools still require individual alignment. |
| `src/tools/read-file.ts` | `internal/tools/read_file.go` | Partially aligned | Text reads, size limits, paging, cancellation, and concurrent-modification checks corrected. Image support is deferred with the images module. |
| `src/tools/registry.ts` | `internal/tools/registry.go` | Corrected | Ordered tool pools, borrowed/owned lifetimes, disposal, reloads, protocol schemas, and independent discovery aligned. Validated by registry_alignment_test.go. |
| `src/tools/search-output.ts` | `internal/tools/search_output.go` | Corrected | Match-count and UTF-8 byte budgets aligned. Validated by search_alignment_test.go. |
| `src/tools/shell-background.ts` | `internal/tools/shell_background.go`, `internal/tools/powershell.go` | Corrected | File capture, UTF-8 previews, background results, host injection, and exit cleanup aligned. Validated by shell_alignment_test.go. |
| `src/tools/shell-output.ts` | `internal/tools/search_output.go`, `internal/tools/shell_background.go` | Corrected | UTF-8 truncation and output formatting aligned. Validated by shell_alignment_test.go. |
| `src/tools/snippets.ts` | `internal/tools` | Pending review |  |
| `src/tools/synthetic-output.ts` | `internal/tools` | Pending review |  |
| `src/tools/task-output.ts` | `internal/tools/task_output.go` | Corrected | Strict arguments, bounded waits, independent notifications, and task snapshots aligned. Validated by shell_alignment_test.go. |
| `src/tools/tool-search.ts` | `internal/tools/tool_search.go` | Corrected | The 50-result limit, select ordering, native text/tool_reference output, and McpCall routing aligned. Validated by tool_search_test.go. |
| `src/tools/types.ts` | `internal/tools/tool.go`, `internal/tools/execution_coordinator.go`, `internal/conversation/content_blocks.go` | Partially aligned | M2: Full source read. Runtime plan/permission/task context and result metadata extended; persisted tool-result content normalization added. MCP App metadata and complete rich-content/provider/host handling remain outstanding. |
| `src/tools/web-fetch.ts` | `internal/tools/web_fetch.go` | Language-specific difference | M1: full source compared. HTTP(S), redirects, text/binary checks, cancellation, 60s timeout, 10MB response cap, UTF-16-safe 100K output cap, 15-minute cache, and 50MB oldest-first cache budget implemented. Go uses html-to-markdown/v2 instead of Turndown: Markdown formatting can differ, including fenced code blocks. Network/content behavior validated with local HTTP fixtures; byte-identical HTML conversion is not claimed. |
| `src/tools/write-file.ts` | `internal/tools/write_file.go` | Corrected | Read-before-write gating, rejection of missing content, atomic writes, and returned line counts aligned. Validated by file_tools_alignment_test.go. |
| `src/ui/agent-activity.tsx` | `internal/ui` | Pending review |  |
| `src/ui/agent-status.tsx` | `internal/ui` | Pending review |  |
| `src/ui/agent-tool-progress.tsx` | `internal/ui` | Pending review |  |
| `src/ui/agents-dialog.tsx` | `internal/ui` | Pending review |  |
| `src/ui/app.tsx` | `internal/ui` | Partially aligned | M2: Complete reference-file comparison remains outstanding. /plan and /review dispatch, plan approval, scoped user input records and conversation reset/resume integrations changed; unrelated UI behavior is not certified. |
| `src/ui/ask-user-dialog.tsx` | `internal/ui` | Pending review |  |
| `src/ui/chat.tsx` | `internal/ui` | Pending review |  |
| `src/ui/constants.ts` | `internal/ui` | Pending review |  |
| `src/ui/cursor-text.tsx` | `internal/ui` | Pending review |  |
| `src/ui/diff-render.tsx` | `internal/ui` | Pending review |  |
| `src/ui/footer.tsx` | `internal/ui` | Pending review |  |
| `src/ui/input-draft.ts` | `internal/ui` | Pending review |  |
| `src/ui/input-navigation.ts` | `internal/ui` | Pending review |  |
| `src/ui/input-paste.ts` | `internal/ui` | Pending review |  |
| `src/ui/input.tsx` | `internal/ui` | Pending review |  |
| `src/ui/interaction-dock.tsx` | `internal/ui` | Pending review |  |
| `src/ui/interaction-summary.ts` | `internal/ui` | Pending review |  |
| `src/ui/interrupt-scope.ts` | `internal/ui/interrupt.go` | Partially aligned | Full source reviewed. Esc/Ctrl+C foreground cancellation and prompt dismissal corrected. Queue and busy-state behavior await the complete UI review. |
| `src/ui/list-window.ts` | `internal/ui` | Pending review |  |
| `src/ui/markdown.ts` | `internal/ui` | Pending review |  |
| `src/ui/marked-terminal.ts` | `internal/ui` | Pending review |  |
| `src/ui/model-select.tsx` | `internal/ui` | Pending review |  |
| `src/ui/pending-queue.tsx` | `internal/ui` | Pending review |  |
| `src/ui/permission-dialog.tsx` | `internal/ui` | Pending review |  |
| `src/ui/permission-request.ts` | `internal/ui` | Pending review |  |
| `src/ui/plan-approval.tsx` | `internal/ui/plan_approval.go` | Partially aligned | M2: Full source read. Manual/YOLO/feedback choices, Esc manual approval, blank-feedback retention, Shift+Tab feedback and plan-exit reminders corrected. Actual terminal interaction/rendering still needs a dedicated UI pass. |
| `src/ui/provider-login.tsx` | `internal/ui` | Pending review |  |
| `src/ui/provider-select.tsx` | `internal/ui` | Pending review |  |
| `src/ui/rewind-dialog.tsx` | `internal/ui` | Pending review |  |
| `src/ui/selector-frame.tsx` | `internal/ui` | Pending review |  |
| `src/ui/selector-layout.ts` | `internal/ui` | Pending review |  |
| `src/ui/selector-list.tsx` | `internal/ui` | Pending review |  |
| `src/ui/selector-search.ts` | `internal/ui` | Pending review |  |
| `src/ui/session-selector.tsx` | `internal/ui` | Pending review | M2: Complete reference-file comparison remains outstanding. Resume now reuses the conversation object and the shared metadata-preserving session reconstruction; this does not certify selector UI parity. |
| `src/ui/status-border.tsx` | `internal/ui` | Pending review |  |
| `src/ui/streaming-markdown.ts` | `internal/ui` | Pending review |  |
| `src/ui/styles.ts` | `internal/ui` | Pending review |  |
| `src/ui/syntax-highlight.ts` | `internal/ui` | Pending review |  |
| `src/ui/table-layout.ts` | `internal/ui` | Pending review |  |
| `src/ui/terminal-input.ts` | `internal/ui` | Pending review |  |
| `src/ui/terminal-layout.tsx` | `internal/ui` | Pending review |  |
| `src/ui/terminal-output.ts` | `internal/ui` | Pending review |  |
| `src/ui/terminal-text.ts` | `internal/ui` | Pending review |  |
| `src/ui/terminal-theme.ts` | `internal/ui` | Pending review |  |
| `src/ui/text-field.tsx` | `internal/ui` | Pending review |  |
| `src/ui/thinking-block.tsx` | `internal/ui` | Pending review |  |
| `src/ui/thinking-select.tsx` | `internal/ui` | Pending review |  |
| `src/ui/todo-progress.tsx` | `internal/ui` | Pending review |  |
| `src/ui/tool-display.tsx` | `internal/ui` | Pending review |  |
| `src/ui/tool-preview.ts` | `internal/ui` | Pending review |  |
| `src/ui/transcript.tsx` | `internal/ui` | Pending review |  |
| `src/ui/update-notice.tsx` | `internal/ui` | Pending review |  |
| `src/ui/use-agent-output.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-follow-up-queue.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-ide-input.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-notification-wakeup.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-subagents.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-task-progress.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-teammate-states.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-terminal-controls.ts` | `internal/ui/ui.go` | Partially aligned | Full source reviewed. Ctrl+B is wired in. The idle double-Ctrl+C exit sequence and its two-second timer remain outstanding. |
| `src/ui/use-terminal-layout.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-update-notice.ts` | `internal/ui` | Pending review |  |
| `src/ui/use-user-bash.ts` | `internal/ui` | Pending review |  |
| `src/ui/workflows-dialog.tsx` | `internal/ui` | Pending review |  |
| `src/utils/constants.ts` | `internal/utils` | Pending review |  |
| `src/utils/file-scan.ts` | `internal/ui`, `internal/remote` | Pending review | Reference source read. Shared scanning and completion utilities have not yet been extracted. |
| `src/utils/index.ts` | `internal/utils` | Pending review |  |
| `src/utils/paths.ts` | `internal/utils/paths.go` | Corrected | Compared line by line. Joining paths through dangling absolute symlinks corrected. Validated by paths_test.go. |
| `src/utils/public-http.ts` | `internal/utils` | Pending review |  |
| `src/utils/redact.ts` | `internal/utils` | Pending review |  |
| `src/utils/slug.ts` | `internal/utils` | Pending review |  |
| `src/utils/xml.ts` | `internal/utils` | Pending review |  |
| `src/version.ts` | `cmd/yukino` | Pending review |  |
| `src/websocket-security.ts` | `internal/remote/access.go` | Partially aligned | M2: Full source compared. Per-server random token, exact socket path, constant-time token check and HTTP same-host origin policy covered by access_test.go. Origin host canonicalization, including case and default-port handling, still needs alignment with the reference URL parser. |
| `src/worktree/constants.ts` | `internal/worktree` | Pending review |  |
| `src/worktree/index.ts` | `internal/worktree` | Pending review | M2: Complete reference-file comparison remains outstanding. Agent worktree creation accepts the actual execution cwd; owned roots, live checker cwd and the full worktree lifecycle remain open. |
| `src/worktree/prompts.ts` | `internal/worktree` | Pending review |  |
