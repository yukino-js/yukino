package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/yukino-js/yukino/yukino/internal/lsp"
)

const (
	MaxCachedDocuments         = 40
	MaxCachedDocumentBytes     = 8 * 1024 * 1024
	MaxCachedDiagnosticBytes   = 2 * 1024 * 1024
	LSPDescription             = "Query a configured language server for definitions, references, hover, symbols, implementations, type definitions, call hierarchy, or diagnostics. file_path resolves against the current working directory; line and character are 1-based UTF-16 positions. Returned ranges use the LSP standard (0-based UTF-16) and file URIs. This tool cannot edit files or run arbitrary commands. Servers must be explicitly configured in lsp_servers; no automatic installation."
	LSPDisposedError           = "LSP tool is disposed"
	LSPInterruptedError        = "LSP operation interrupted"
	LSPServerExitedError       = "LSP server exited; retry to start a fresh server"
	LSPDocumentTooLargeError   = "LSP requires a regular file no larger than 8MB"
	LSPPositionOutOfRangeError = "LSP position is outside the document"
	LSPFileTooLargeError       = "LSP requires a regular file no larger than 10MB"
)

var LSPOperations = []string{"definition", "references", "hover", "documentSymbol", "workspaceSymbol", "implementation", "typeDefinition", "prepareCallHierarchy", "incomingCalls", "outgoingCalls", "diagnostics"}

type lspDocument struct {
	text    string
	version int
}
type lspDiagnostic struct {
	version   int
	items     []any
	bytes     int
	truncated bool
}
type lspSession struct {
	client          *lsp.Client
	queue           chan struct{}
	mu              sync.Mutex
	documents       map[string]lspDocument
	documentOrder   []string
	diagnostics     map[string]lspDiagnostic
	diagnosticOrder []string
	changed         chan struct{}
}
type lspSessionEntry struct {
	ready   chan struct{}
	session *lspSession
	err     error
}
type LSPTool struct {
	Configs  []lsp.ServerConfig
	mu       sync.Mutex
	sessions map[string]*lspSessionEntry
	disposed bool
}

func (t *LSPTool) Name() string           { return "LSP" }
func (t *LSPTool) Description() string    { return LSPDescription }
func (t *LSPTool) Category() ToolCategory { return CategoryRead }
func (t *LSPTool) Schema() map[string]any {
	return map[string]any{"name": t.Name(), "description": t.Description(), "input_schema": map[string]any{"type": "object", "properties": map[string]any{"operation": map[string]any{"type": "string", "enum": LSPOperations}, "file_path": map[string]any{"type": "string"}, "line": map[string]any{"type": "integer", "minimum": 1}, "character": map[string]any{"type": "integer", "minimum": 1}, "query": map[string]any{"type": "string"}}, "required": []string{"operation", "file_path"}, "additionalProperties": false}}
}
func removeLSPKey(order []string, key string) []string {
	return slices.DeleteFunc(order, func(value string) bool { return value == key })
}
func (s *lspSession) onNotification(method string, value any) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	params, ok := value.(map[string]any)
	if !ok {
		return
	}
	uri, ok := params["uri"].(string)
	if !ok {
		return
	}
	diagnostics, ok := params["diagnostics"].([]any)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, exists := s.documents[uri]
	if !exists {
		return
	}
	if version, ok := params["version"].(float64); ok && version != float64(doc.version) {
		return
	}
	items := []any{}
	bytes := 0
	for _, item := range diagnostics {
		encoded, err := json.Marshal(item)
		if err != nil {
			return
		}
		if bytes+len(encoded) > MaxCachedDiagnosticBytes {
			break
		}
		items = append(items, item)
		bytes += len(encoded)
	}
	delete(s.diagnostics, uri)
	s.diagnosticOrder = removeLSPKey(s.diagnosticOrder, uri)
	total := bytes
	for _, cached := range s.diagnostics {
		total += cached.bytes
	}
	for total > MaxCachedDiagnosticBytes && len(s.diagnosticOrder) > 0 {
		oldest := s.diagnosticOrder[0]
		total -= s.diagnostics[oldest].bytes
		delete(s.diagnostics, oldest)
		s.diagnosticOrder = s.diagnosticOrder[1:]
	}
	s.diagnostics[uri] = lspDiagnostic{version: doc.version, items: items, bytes: bytes, truncated: len(items) != len(diagnostics)}
	s.diagnosticOrder = append(s.diagnosticOrder, uri)
	close(s.changed)
	s.changed = make(chan struct{})
}
func (t *LSPTool) session(ctx context.Context, config lsp.ServerConfig, cwd string, restart bool) (*lspSession, error) {
	key := cwd + "\x00" + config.Name
	t.mu.Lock()
	if t.disposed {
		t.mu.Unlock()
		return nil, errors.New(LSPDisposedError)
	}
	if t.sessions == nil {
		t.sessions = map[string]*lspSessionEntry{}
	}
	entry := t.sessions[key]
	if entry == nil {
		entry = &lspSessionEntry{ready: make(chan struct{})}
		t.sessions[key] = entry
		go func() {
			client, err := lsp.NewClient(config, cwd, RegisterExitCleanup)
			if err == nil {
				session := &lspSession{client: client, queue: make(chan struct{}, 1), documents: map[string]lspDocument{}, diagnostics: map[string]lspDiagnostic{}, changed: make(chan struct{})}
				client.OnNotification(session.onNotification)
				err = client.Initialize()
				if err == nil {
					entry.session = session
				} else {
					_ = client.Dispose()
				}
			}
			entry.err = err
			t.mu.Lock()
			if err != nil && t.sessions[key] == entry {
				delete(t.sessions, key)
			}
			close(entry.ready)
			t.mu.Unlock()
		}()
	}
	t.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, errors.New(LSPInterruptedError)
	case <-entry.ready:
	}
	if entry.err != nil {
		return nil, entry.err
	}
	if restart && entry.session.client.IsClosed() {
		_ = entry.session.client.Dispose()
		t.mu.Lock()
		if t.sessions[key] == entry {
			delete(t.sessions, key)
		}
		t.mu.Unlock()
		return t.session(ctx, config, cwd, false)
	}
	return entry.session, nil
}
func (s *lspSession) syncDocument(path, text, language string) (string, error) {
	uri := lsp.FileURI(path)
	s.mu.Lock()
	previous, exists := s.documents[uri]
	var notification string
	var params map[string]any
	if !exists || previous.text != text {
		version := previous.version + 1
		s.documents[uri] = lspDocument{text, version}
		delete(s.diagnostics, uri)
		s.diagnosticOrder = removeLSPKey(s.diagnosticOrder, uri)
		if !exists {
			notification = "textDocument/didOpen"
			params = map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": language, "version": version, "text": text}}
		} else {
			notification = "textDocument/didChange"
			params = map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []any{map[string]any{"text": text}}}
		}
	}
	s.documentOrder = append(removeLSPKey(s.documentOrder, uri), uri)
	total := 0
	for _, doc := range s.documents {
		total += len(doc.text)
	}
	var evicted []string
	for len(s.documents) > MaxCachedDocuments || total > MaxCachedDocumentBytes {
		if len(s.documentOrder) < 2 {
			break
		}
		oldest := s.documentOrder[0]
		s.documentOrder = s.documentOrder[1:]
		total -= len(s.documents[oldest].text)
		delete(s.documents, oldest)
		delete(s.diagnostics, oldest)
		s.diagnosticOrder = removeLSPKey(s.diagnosticOrder, oldest)
		evicted = append(evicted, oldest)
	}
	s.mu.Unlock()
	if notification != "" {
		if err := s.client.Notify(notification, params); err != nil {
			return "", err
		}
	}
	for _, oldest := range evicted {
		_ = s.client.Notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": oldest}})
	}
	return uri, nil
}
func (s *lspSession) query(ctx context.Context, config lsp.ServerConfig, path string, args map[string]any) (any, error) {
	if ctx.Err() != nil {
		return nil, errors.New(LSPInterruptedError)
	}
	if s.client.IsClosed() {
		return nil, errors.New(LSPServerExitedError)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxCachedDocumentBytes {
		return nil, errors.New(LSPDocumentTooLargeError)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, errors.New(LSPInterruptedError)
	}
	text := strings.ToValidUTF8(string(data), "�")
	uri, err := s.syncDocument(path, text, config.Languages[filepath.Ext(path)])
	if err != nil {
		return nil, err
	}
	line, character := intArg(args, "line", 1)-1, intArg(args, "character", 1)-1
	if _, exists := args["line"]; exists {
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		if line >= len(lines) || character > len(utf16.Encode([]rune(lines[line]))) {
			return nil, errors.New(LSPPositionOutOfRangeError)
		}
	}
	textDocument := map[string]any{"uri": uri}
	position := map[string]any{"line": line, "character": character}
	operation := args["operation"].(string)
	request := func(method string, params any) (any, error) { return s.client.Request(ctx, method, params, 0) }
	switch operation {
	case "workspaceSymbol":
		return request("workspace/symbol", map[string]any{"query": args["query"]})
	case "documentSymbol":
		return request("textDocument/documentSymbol", map[string]any{"textDocument": textDocument})
	case "diagnostics":
		if capability := s.client.Capabilities()["diagnosticProvider"]; capability != nil && capability != false {
			return request("textDocument/diagnostic", map[string]any{"textDocument": textDocument})
		}
		timeout := 2 * time.Second
		if config.TimeoutMs != 0 {
			timeout = min(timeout, time.Duration(config.TimeoutMs)*time.Millisecond)
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		for {
			s.mu.Lock()
			cached, exists := s.diagnostics[uri]
			changed := s.changed
			s.mu.Unlock()
			if exists {
				return map[string]any{"items": cached.items, "pending": false, "truncated": cached.truncated}, nil
			}
			select {
			case <-ctx.Done():
				return nil, errors.New(LSPInterruptedError)
			case <-timer.C:
				return map[string]any{"items": []any{}, "pending": true, "truncated": false}, nil
			case <-changed:
			}
		}
	case "incomingCalls", "outgoingCalls":
		prepared, err := request("textDocument/prepareCallHierarchy", map[string]any{"textDocument": textDocument, "position": position})
		if err != nil {
			return nil, err
		}
		items, _ := prepared.([]any)
		results := []any{}
		for _, item := range items {
			calls, err := request("callHierarchy/"+operation, map[string]any{"item": item})
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{"item": item, "calls": calls})
		}
		return results, nil
	default:
		params := map[string]any{"textDocument": textDocument, "position": position}
		if operation == "references" {
			params["context"] = map[string]any{"includeDeclaration": true}
		}
		return request("textDocument/"+operation, params)
	}
}
func validLSPArgs(args map[string]any) error {
	for key := range args {
		if !slices.Contains([]string{"operation", "file_path", "line", "character", "query"}, key) {
			return fmt.Errorf("Unrecognized key: %s", key)
		}
	}
	operation, _ := args["operation"].(string)
	path, _ := args["file_path"].(string)
	if !slices.Contains(LSPOperations, operation) || path == "" {
		return errors.New("operation and file_path are required")
	}
	for _, key := range []string{"line", "character"} {
		if value, exists := args[key]; exists {
			n := intArg(args, key, -1)
			if n < 1 {
				return fmt.Errorf("%s must be a positive integer", key)
			}
			switch number := value.(type) {
			case float64:
				if float64(n) != number {
					return fmt.Errorf("%s must be a positive integer", key)
				}
			case int, int64:
			default:
				return fmt.Errorf("%s must be a positive integer", key)
			}
		}
	}
	if query, exists := args["query"]; exists {
		if _, ok := query.(string); !ok {
			return errors.New("query must be a string")
		}
	}
	if !slices.Contains([]string{"documentSymbol", "workspaceSymbol", "diagnostics"}, operation) && (args["line"] == nil || args["character"] == nil) {
		return errors.New("This operation requires 1-based line and character (UTF-16).")
	}
	if operation == "workspaceSymbol" {
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return errors.New("workspaceSymbol requires a non-empty query")
		}
	}
	return nil
}
func (t *LSPTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	if err := validLSPArgs(args); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}
	}
	fail := func(err error) ToolResult { return ToolResult{Output: "Error: " + err.Error(), IsError: true} }
	if ctx.Err() != nil {
		return fail(errors.New(LSPInterruptedError))
	}
	cwd, err := filepath.Abs(workingDirectory(ctx))
	if err != nil {
		return fail(err)
	}
	path := toolPath(ctx, args["file_path"].(string))
	var config *lsp.ServerConfig
	for i := range t.Configs {
		if _, exists := t.Configs[i].Languages[filepath.Ext(path)]; exists {
			config = &t.Configs[i]
			break
		}
	}
	if config == nil {
		return fail(fmt.Errorf("No LSP server configured for '%s'. Add an installed server to lsp_servers.", filepath.Ext(path)))
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Size() > 10*1024*1024 {
		return fail(errors.New(LSPFileTooLargeError))
	}
	s, err := t.session(ctx, *config, cwd, true)
	if err != nil {
		return fail(err)
	}
	select {
	case s.queue <- struct{}{}:
	case <-ctx.Done():
		return fail(errors.New(LSPInterruptedError))
	}
	defer func() { <-s.queue }()
	result, err := s.query(ctx, *config, path, args)
	if err != nil {
		return fail(err)
	}
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fail(err)
	}
	text := string(output)
	if len(utf16.Encode([]rune(text))) > 100000 {
		text = truncateUTF16(text, 100000) + "\n[Truncated; narrow the query.]"
	}
	return ToolResult{Output: text}
}
func (t *LSPTool) Dispose() error {
	t.mu.Lock()
	t.disposed = true
	entries := make([]*lspSessionEntry, 0, len(t.sessions))
	for _, entry := range t.sessions {
		entries = append(entries, entry)
	}
	clear(t.sessions)
	t.mu.Unlock()
	var wg sync.WaitGroup
	for _, entry := range entries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-entry.ready
			if entry.session != nil {
				_ = entry.session.client.Dispose()
			}
		}()
	}
	wg.Wait()
	return nil
}
func truncateUTF16(text string, limit int) string {
	units := 0
	for i, r := range text {
		count := 1
		if r > 0xffff {
			count = 2
		}
		if units+count > limit {
			return text[:i]
		}
		units += count
	}
	return text
}
