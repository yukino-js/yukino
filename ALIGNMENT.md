# Existing Module Alignment Ledger

The reference implementation is `~/github/yukino-code/apps/yukino`, at commit `8dbf96950d45fac1c7c68451478716898b35810d`.

This phase aligns existing Go modules with the reference on a file-by-file basis. Missing files and behavioral gaps within those modules are in scope. Entirely absent modules, including a2a, images, goal, workflow, telemetry, vscode, update, and logger, are deferred. Implementation differences are acceptable only where TypeScript and Go require different language or runtime choices. Backward compatibility, deprecated APIs, and compatibility shims must not be retained.

Every previously tracked reference file retains its own record. A module-level counterpart identifies the area to review; it does not certify the entire Go package. Full alignment requires reading the complete reference file, comparing its implementation, and validating the resulting behavior. Partial progress and unresolved differences are recorded explicitly. Shared browser inputs follow the artifact delivery policy below.

Current inventory: **321 records**: 25 corrected, 4 reviewed, 24 partially aligned, 212 pending review, 1 deferred, 49 shared browser build inputs, and 6 synchronized artifacts. **Full alignment is not complete.**

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

## File-by-File Ledger

| Reference file | Go counterpart or shared artifact | Status | Differences and validation |
| --- | --- | --- | --- |
| `src/agent/constants.ts` | `internal/agent` | Pending review |  |
| `src/agent/events.ts` | `internal/agent` | Pending review |  |
| `src/agent/index.ts` | `internal/agent` | Pending review |  |
| `src/agent/prompts.ts` | `internal/agent` | Pending review |  |
| `src/agent/streaming-executor.ts` | `internal/agent` | Pending review |  |
| `src/bootstrap/exit-cleanup.ts` | `internal/tools/exit_cleanup.go`, `cmd/yukino/main.go` | Corrected | Registered synchronous, idempotent cleanup and integrated it with normal, signal-driven, and error exits. |
| `src/bootstrap/index.ts` | `cmd/yukino`, `internal/ui`, `internal/remote` | Pending review |  |
| `src/bootstrap/interaction-summary.ts` | `cmd/yukino`, `internal/ui`, `internal/remote` | Pending review |  |
| `src/bootstrap/sandbox.ts` | `cmd/yukino`, `internal/ui`, `internal/remote` | Pending review |  |
| `src/bootstrap/tool-registry.ts` | `internal/tools/tool.go`, `internal/ui/agent_setup.go` and related files | Partially aligned | Full source reviewed. PowerShell, TaskOutput, and background task management are wired in. TodoWrite, LSP, WebFetch, and EnterPlanMode remain outstanding. |
| `src/cli.ts` | `cmd/yukino` | Pending review |  |
| `src/commands/commands.ts` | `internal/commands` | Pending review |  |
| `src/commands/constants.ts` | `internal/commands` | Pending review |  |
| `src/commands/index.ts` | `internal/commands` | Pending review |  |
| `src/commands/loader.ts` | `internal/commands` | Pending review |  |
| `src/commands/prompts.ts` | `internal/commands` | Pending review |  |
| `src/commands/subagent.ts` | `internal/commands`, `internal/ui` | Pending review | Reference source read; the corresponding Go execution paths still require a file-by-file comparison. |
| `src/commands/usage-tracker.ts` | `internal/commands` | Pending review |  |
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
| `src/conversation/constants.ts` | `internal/conversation` | Pending review |  |
| `src/conversation/index.ts` | `internal/conversation` | Pending review |  |
| `src/conversation/pairing.ts` | `internal/conversation` | Pending review |  |
| `src/conversation/prompts.ts` | `internal/conversation` | Pending review |  |
| `src/conversation/user-bash.ts` | `internal/conversation` | Pending review |  |
| `src/file-history/constants.ts` | `internal/file_history` | Pending review |  |
| `src/file-history/index.ts` | `internal/file_history` | Pending review |  |
| `src/history/constants.ts` | `internal/history/history.go` | Reviewed | Values, filenames, and messages checked individually against the reference. |
| `src/history/index.ts` | `internal/history/history.go`, `internal/history/signature_*.go` | Partially aligned | Full source reviewed. Platform-specific cache identity and ctime handling corrected. Strict checkpoint validation and error propagation remain outstanding. |
| `src/hooks/constants.ts` | `internal/hooks` | Pending review |  |
| `src/hooks/index.ts` | `internal/hooks` | Pending review |  |
| `src/index.ts` | `cmd/yukino` | Pending review |  |
| `src/llm/anthropic.ts` | `internal/llm` | Pending review |  |
| `src/llm/client.ts` | `internal/llm` | Pending review |  |
| `src/llm/constants.ts` | `internal/llm` | Pending review |  |
| `src/llm/errors.ts` | `internal/llm` | Pending review |  |
| `src/llm/events.ts` | `internal/llm` | Pending review |  |
| `src/llm/index.ts` | `internal/llm` | Pending review |  |
| `src/llm/model-discovery.ts` | `internal/llm` | Pending review |  |
| `src/llm/openai.ts` | `internal/llm` | Pending review |  |
| `src/llm/retry.ts` | `internal/llm` | Pending review |  |
| `src/lsp/client.ts` | `internal/lsp` | Pending review |  |
| `src/lsp/config.ts` | `internal/lsp` | Pending review |  |
| `src/lsp/constants.ts` | `internal/lsp` | Pending review |  |
| `src/lsp/index.ts` | `internal/lsp` | Pending review |  |
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
| `src/permissions/index.ts` | `internal/permissions/permissions.go`, `internal/permissions/mode.go` | Partially aligned | Full source reviewed. Rule-loading failures now deny access; dynamic parent/child modes and plan-mode restoration corrected. OwnedWorktrees, isolated roots, and parts of the lifecycle remain outstanding. |
| `src/permissions/request.ts` | `internal/agent/agent.go` | Partially aligned | Cancellation while waiting and allowAlways without rememberPatterns checked. Host forwarding of delegated permission requests remains outstanding. |
| `src/permissions/shell.ts` | `internal/permissions/shell.go` | Reviewed | Compared line by line. Shell segmentation and Unicode whitespace semantics corrected. |
| `src/plan-file/constants.ts` | `internal/plan_file` | Pending review |  |
| `src/plan-file/index.ts` | `internal/plan_file` | Pending review |  |
| `src/print-mode.ts` | `cmd/yukino` | Pending review |  |
| `src/prompt/builder.ts` | `internal/prompt` | Pending review |  |
| `src/prompt/constants.ts` | `internal/prompt` | Pending review |  |
| `src/prompt/coordinator.ts` | `internal/prompt` | Pending review |  |
| `src/prompt/index.ts` | `internal/prompt` | Pending review |  |
| `src/prompt/plan-mode.ts` | `internal/prompt` | Pending review |  |
| `src/prompt/prompts.ts` | `internal/prompt` | Pending review |  |
| `src/prompt/sections.ts` | `internal/prompt` | Pending review |  |
| `src/prompt/tools.ts` | `internal/prompt` | Pending review |  |
| `src/recover.ts` | `cmd/yukino` | Pending review |  |
| `src/remote/address.ts` | `internal/remote` | Pending review |  |
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
| `src/remote/server.ts` | `internal/remote/server.go`, `internal/remote/web.go` | Partially aligned | Embedded bundle serving, asset MIME types, and SPA fallback corrected; web_test.go exercises every bundled file. Remaining HTTP routes, WebSocket protocol, and runtime behavior still require a complete file review. |
| `src/remote/session-state.ts` | `internal/remote` | Pending review |  |
| `src/sandbox/bwrap.ts` | `internal/sandbox/bwrap.go` | Corrected | Structured argv, working-directory resolution, and --die-with-parent aligned. Validated by sandbox_test.go. |
| `src/sandbox/constants.ts` | `internal/sandbox/seatbelt.go` | Corrected | Fixed executable paths and invalid-path-character errors aligned. Validated by sandbox_test.go. |
| `src/sandbox/index.ts` | `internal/sandbox/sandbox.go` | Corrected | Structured Prepare arguments aligned. Go type assertions express optional cleanup support. Validated by go test ./internal/sandbox. |
| `src/sandbox/seatbelt.ts` | `internal/sandbox/seatbelt.go` | Corrected | Path aliases, profile escaping, /dev/null handling, and deny rules for literal paths and subpaths aligned. Validated by sandbox_test.go. |
| `src/session/constants.ts` | `internal/session` | Pending review |  |
| `src/session/index.ts` | `internal/session` | Pending review |  |
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
| `src/subagent/agent-tool.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/constants.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/definition.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/index.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/loader.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/plan.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/prompts.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/spawn.ts` | `internal/subagent` | Pending review |  |
| `src/subagent/task-manager.ts` | `internal/subagent/task_manager.go` | Partially aligned | Task lifecycle, retention after cancellation, waits, notifications, retention limits, and session restoration corrected. Complete propagation of persistence failures remains outstanding. Validated by task_manager_test.go. |
| `src/subagent/task-store.ts` | `internal/subagent/task_store.go` | Corrected | Private, atomic JSON records, ID validation, and restoration ordering aligned. Validated by task_manager_test.go. |
| `src/subagent/tool-filter.ts` | `internal/subagent/tool_filter.go` | Partially aligned | Definition-level MCP restrictions, loading-state copies, borrowed tools, and TaskStop/LoadSkill scoping corrected. Independent TodoWrite boards await todo-module alignment. |
| `src/teams/constants.ts` | `internal/teams` | Pending review |  |
| `src/teams/coordinator.ts` | `internal/teams` | Pending review |  |
| `src/teams/file-lock.ts` | `internal/teams` | Pending review |  |
| `src/teams/file-mailbox.ts` | `internal/teams` | Pending review |  |
| `src/teams/index.ts` | `internal/teams` | Pending review |  |
| `src/teams/progress.ts` | `internal/teams` | Pending review |  |
| `src/teams/prompts.ts` | `internal/teams` | Pending review |  |
| `src/teams/protocol.ts` | `internal/teams` | Pending review |  |
| `src/teams/registry.ts` | `internal/teams` | Pending review |  |
| `src/teams/shared-task.ts` | `internal/teams` | Pending review |  |
| `src/teams/task-stop.ts` | `internal/teams/task_stop.go` | Partially aligned | Background task_id handling, cancellation waits, and delegated restrictions implemented. Disambiguation of identical names across teams awaits teams-module alignment. |
| `src/teams/task-tools.ts` | `internal/teams` | Pending review |  |
| `src/teams/team-file.ts` | `internal/teams` | Pending review |  |
| `src/teams/tools.ts` | `internal/teams` | Pending review |  |
| `src/theme/constants.ts` | `internal/ui/theme.go` | Pending review |  |
| `src/theme/index.ts` | `internal/ui/theme.go` | Pending review |  |
| `src/theme/syntax.ts` | `internal/ui/theme.go` | Pending review |  |
| `src/todo/constants.ts` | `internal/todo` | Pending review |  |
| `src/todo/dependencies.ts` | `internal/todo` | Pending review |  |
| `src/todo/index.ts` | `internal/todo` | Pending review |  |
| `src/todo/progress.ts` | `internal/todo` | Pending review |  |
| `src/todo/prompts.ts` | `internal/todo` | Pending review |  |
| `src/todo/store.ts` | `internal/todo` | Pending review |  |
| `src/todo/tools.ts` | `internal/todo` | Pending review |  |
| `src/todo/write.ts` | `internal/todo` | Pending review |  |
| `src/tool-result/constants.ts` | `internal/tool_result` | Pending review |  |
| `src/tool-result/index.ts` | `internal/tool_result` | Pending review |  |
| `src/tool-result/prompts.ts` | `internal/tool_result` | Pending review |  |
| `src/tools/ask-user.ts` | `internal/tools` | Pending review |  |
| `src/tools/atomic-write.ts` | `internal/tools/atomic_write.go` | Corrected | Same-directory temporary files, fsync and rename, symlink handling, and permission preservation aligned. Validated by file_tools_alignment_test.go. |
| `src/tools/bash.ts` | `internal/tools/bash.go`, `internal/tools/shell_background.go` | Partially aligned | Cancellation, background execution, timeouts, persisted output, and sandboxing implemented. Isolated-worktree write roots and temporary-directory integration remain outstanding. |
| `src/tools/computer-use.ts` | `internal/tools` | Pending review |  |
| `src/tools/constants.ts` | `internal/tools/constants.go` and related files | Partially aligned | Full source reviewed. File, search, and shell budgets and error messages implemented. LSP, WebFetch, and ComputerUse dependencies remain outstanding. |
| `src/tools/diff.ts` | `internal/tools/diff.go` | Corrected | Separate edit regions, accurate addition/deletion counts, new-file line numbers, and truncation aligned. Validated by diff_test.go and file_tools_alignment_test.go. |
| `src/tools/edit-file-diagnostics.ts` | `internal/tools/edit_file_diagnostics.go` | Corrected | Whitespace candidates and Unicode difference positions aligned. Go uses rune-based character indexing. Validated by file_tools_alignment_test.go. |
| `src/tools/edit-file.ts` | `internal/tools/edit_file.go` | Corrected | Only the batch edits interface is accepted. Original-text matching, atomic failure, and preservation of CRLF and stray carriage returns aligned. Validated by file_tools_alignment_test.go. |
| `src/tools/enter-plan-mode.ts` | `internal/tools` | Pending review |  |
| `src/tools/enter-worktree.ts` | `internal/tools` | Pending review |  |
| `src/tools/execution-coordinator.ts` | `internal/tools/execution_coordinator.go` | Partially aligned | Workspace write serialization and permission rechecks after waiting implemented. Runtime working-directory changes for worktrees remain outstanding. |
| `src/tools/exit-code-hints.ts` | `internal/tools/exit_code_hints.go` | Corrected | Quoted command segments, skipped variable assignments, and command/signal exit hints aligned. Validated by shell_alignment_test.go. |
| `src/tools/exit-plan-mode.ts` | `internal/tools` | Pending review |  |
| `src/tools/exit-worktree.ts` | `internal/tools` | Pending review |  |
| `src/tools/file-mutation-queue.ts` | `internal/tools/execution_coordinator.go` | Corrected | Mutations are serialized by canonical path; waiting can be cancelled. Validated by concurrency regression tests. |
| `src/tools/file-state-cache.ts` | `internal/tools/file_state_cache.go` | Corrected | Canonical keys, rejection after deletion or backward timestamp changes, and has/delete/clear operations aligned. Validated by file_tools_alignment_test.go. |
| `src/tools/glob.ts` | `internal/tools/glob.go`, `internal/tools/search_output.go` | Partially aligned | Working-directory handling, brace expansion, globstar, dotfiles, limits, and sorting implemented. The minimatch extglob syntax remains unsupported. |
| `src/tools/goal.ts` | — | Deferred: missing module | Depends on the goal module, which is explicitly outside the scope of this phase. |
| `src/tools/grep.ts` | `internal/tools/grep.go` | Corrected | JavaScript Unicode-regex conversion, timeouts, file/directory handling, binary files, symlinks, and traversal/output budgets aligned. Validated by search_alignment_test.go. |
| `src/tools/index.ts` | `internal/tools` | Partially aligned | Full source reviewed. Go package exports replace the TypeScript barrel; some tools remain unported. |
| `src/tools/is-diff-tool.ts` | `internal/ui/tool_display.go` | Reviewed | Source reviewed. Only EditFile uses diff rendering; integration still requires validation during the complete UI review. |
| `src/tools/lsp.ts` | `internal/tools` | Pending review |  |
| `src/tools/mcp-call.ts` | `internal/tools/mcp_call.go` | Corrected | Calls are restricted to MCP tools on the same server. Argument-object validation and coercion aligned. Validated by mcp_call_test.go and registry_alignment_test.go. |
| `src/tools/powershell.ts` | `internal/tools/powershell.go`, `internal/tools/shell_background.go` | Partially aligned | Shares the shell lifecycle and includes Windows taskkill arguments. PowerShell and Windows integration have not been exercised on this host. |
| `src/tools/prompts.ts` | `internal/tools/descriptions.go` and related files | Partially aligned | Full source reviewed. Descriptions for the six file/search tools and shell tools synchronized. Descriptions for the remaining tools still require individual alignment. |
| `src/tools/read-file.ts` | `internal/tools/read_file.go` | Partially aligned | Text reads, size limits, paging, cancellation, and concurrent-modification checks corrected. Image support is deferred with the images module. |
| `src/tools/registry.ts` | `internal/tools/registry.go` | Corrected | Ordered tool pools, borrowed/owned lifetimes, disposal, reloads, protocol schemas, and independent discovery aligned. Validated by registry_alignment_test.go. |
| `src/tools/search-output.ts` | `internal/tools/search_output.go` | Corrected | Match-count and UTF-8 byte budgets aligned. Validated by search_alignment_test.go. |
| `src/tools/shell-background.ts` | `internal/tools/shell_background.go`, `internal/tools/powershell.go` | Corrected | File capture, UTF-8 previews, background results, host injection, and exit cleanup aligned. Validated by shell_alignment_test.go. |
| `src/tools/shell-output.ts` | `internal/tools/search_output.go`, `internal/tools/shell_background.go` | Corrected | UTF-8 truncation and output formatting aligned. Validated by shell_alignment_test.go. |
| `src/tools/snippets.ts` | `internal/tools` | Pending review |  |
| `src/tools/synthetic-output.ts` | `internal/tools` | Pending review |  |
| `src/tools/task-output.ts` | `internal/tools/task_output.go` | Corrected | Strict arguments, bounded waits, independent notifications, and task snapshots aligned. Validated by shell_alignment_test.go. |
| `src/tools/tool-search.ts` | `internal/tools/tool_search.go` | Corrected | The 50-result limit, select ordering, native text/tool_reference output, and McpCall routing aligned. Validated by tool_search_test.go. |
| `src/tools/types.ts` | `internal/tools/tool.go`, `internal/tools/task_output.go`, `internal/tools/execution_coordinator.go` | Partially aligned | Full source reviewed. Context and background task protocols extended. MCP App content and unified validation of structured content blocks remain outstanding. |
| `src/tools/web-fetch.ts` | `internal/tools` | Pending review |  |
| `src/tools/write-file.ts` | `internal/tools/write_file.go` | Corrected | Read-before-write gating, rejection of missing content, atomic writes, and returned line counts aligned. Validated by file_tools_alignment_test.go. |
| `src/ui/agent-activity.tsx` | `internal/ui` | Pending review |  |
| `src/ui/agent-status.tsx` | `internal/ui` | Pending review |  |
| `src/ui/agent-tool-progress.tsx` | `internal/ui` | Pending review |  |
| `src/ui/agents-dialog.tsx` | `internal/ui` | Pending review |  |
| `src/ui/app.tsx` | `internal/ui` | Pending review |  |
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
| `src/ui/plan-approval.tsx` | `internal/ui` | Pending review |  |
| `src/ui/provider-login.tsx` | `internal/ui` | Pending review |  |
| `src/ui/provider-select.tsx` | `internal/ui` | Pending review |  |
| `src/ui/rewind-dialog.tsx` | `internal/ui` | Pending review |  |
| `src/ui/selector-frame.tsx` | `internal/ui` | Pending review |  |
| `src/ui/selector-layout.ts` | `internal/ui` | Pending review |  |
| `src/ui/selector-list.tsx` | `internal/ui` | Pending review |  |
| `src/ui/selector-search.ts` | `internal/ui` | Pending review |  |
| `src/ui/session-selector.tsx` | `internal/ui` | Pending review |  |
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
| `src/websocket-security.ts` | `cmd/yukino` | Pending review |  |
| `src/worktree/constants.ts` | `internal/worktree` | Pending review |  |
| `src/worktree/index.ts` | `internal/worktree` | Pending review |  |
| `src/worktree/prompts.ts` | `internal/worktree` | Pending review |  |
