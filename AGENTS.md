- 本项目全面对齐 ~/github/yukino-code/apps/yukino
- MUST 1 比 1 对齐, 可以容忍 TS/Golang 语言差异导致的功能取舍与实现差异
- 逐文件 1 比 1 对齐，禁止抽查、禁止猜测

- ZERO backward compatibility. Breaking changes are expected, acceptable, and preferred over legacy support.
- NEVER maintain conditional logic for older versions, dead code, deprecated APIs, or shim layers. Remove them aggressively.
- If fixing a bug or updating/implementing a valuable feature causes old tests to fail, follow ZERO backward compatibility: implement the correct behavior, update or remove obsolete tests, and update all outdated comments. NEVER preserve defective behavior or add compatibility logic merely to satisfy old tests.
- Only add comments where the code is not self-explanatory. Usage of these comments should be rare.

- a2a: https://github.com/a2aproject/a2a-go
- images
- goal
- workflow
- remote: Use internal/remote/browser/dist/index.html
- telemetry: Use telemetry, sentry/go
- vscode
- ...
