package tools

const (
	DefaultReadLimit          = 2000
	MaxReadBytes              = 50 * 1024
	MaxReadFileBytes          = 10 * 1024 * 1024
	MaxSearchOutputBytes      = 50 * 1024
	MaxSearchResults          = 500
	MaxMatchLineBytes         = 2000
	MaxGrepFileBytes          = 25 * 1024 * 1024
	MaxTraversedEntries       = 10000
	MaxTraversalDepth         = 25
	OperationInterruptedError = "Error: operation interrupted"
	FileNotReadError          = "Error: file has not been read yet, read it first before editing."
	FileDeletedError          = "Error: file was deleted or is no longer accessible; read it again before editing."
	FileModifiedError         = "Error: file has been modified since last read, read it again before editing."
)
