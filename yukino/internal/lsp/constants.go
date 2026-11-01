package lsp

import "time"

const (
	FileExtensionRequiredError       = "At least one file extension is required"
	MinTimeoutMs                     = 100
	MaxTimeoutMs                     = 60_000
	MaxHeaderBytes                   = 8192
	MaxMessageBytes                  = 16 * 1024 * 1024
	MaxStderrChars                   = 4000
	DefaultRequestTimeout            = 15 * time.Second
	ShutdownTimeout                  = 500 * time.Millisecond
	KillDelay                        = 500 * time.Millisecond
	RequestInterruptedError          = "LSP request interrupted"
	HeaderTooLargeError              = "LSP header exceeds 8KB"
	ContentLengthExpectedError       = "Expected one Content-Length header"
	MessageTooLargeError             = "LSP message exceeds 16MB"
	InvalidJSONRPCError              = "Invalid JSON-RPC message"
	RequestFailedError               = "LSP request failed"
	UnsupportedMethodError           = "Unsupported read-only client method"
	ApplyEditFailureReason           = "Yukino LSP is read-only; use authorized file tools for edits."
	InvalidInitializeResponseError   = "Invalid LSP initialize response"
	UnsupportedPositionEncodingError = "Only UTF-16 LSP positions are supported"
	ClientDisposedError              = "LSP client disposed"
)
