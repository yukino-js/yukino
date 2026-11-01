本项目全面对齐 ~/github/yukino-code/apps/yukino

逐文件对齐，禁止抽查、禁止猜测

- ZERO backward compatibility. Breaking changes are expected, acceptable, and preferred over legacy support.
- NEVER maintain conditional logic for older versions, dead code, deprecated APIs, or shim layers. Remove them aggressively.
- If fixing a bug or updating/implementing a valuable feature causes old tests to fail, follow ZERO backward compatibility: implement the correct behavior, update or remove obsolete tests, and update all outdated comments. NEVER preserve defective behavior or add compatibility logic merely to satisfy old tests.
- Only add comments where the code is not self-explanatory. Usage of these comments should be rare.

其中 http 框架使用 github.com/hangtiancheng/yukino.go/libs/yukino_http v0.0.1

yukino_http 可以调用 yukino-http 技能, 源代码位于: ~/github/yukino.go/libs/yukino_http

可能使用到的依赖:

github.com/anthropics/anthropic-sdk-go
github.com/getsentry/sentry-go
github.com/goccy/go-yaml
github.com/google/uuid
github.com/klauspost/compress
github.com/modelcontextprotocol/go-sdk
github.com/openai/openai-go
github.com/prometheus/client_golang
github.com/prometheus/common
github.com/robfig/cron/v3
github.com/twmb/murmur3
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
go.opentelemetry.io/otel
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
go.opentelemetry.io/otel/exporters/stdout/stdouttrace
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/otel/trace
go.uber.org/dig
go.uber.org/mock
go.uber.org/zap
golang.org/x/crypto
golang.org/x/sys
google.golang.org/grpc
google.golang.org/protobuf
