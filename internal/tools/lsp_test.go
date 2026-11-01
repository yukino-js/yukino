package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/lsp"
)

func newTestLSP(t *testing.T, options map[string]any) (*LSPTool, context.Context, string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the shared reference LSP fixture")
	}
	fixture, err := filepath.Abs("testdata/lsp-server.mjs")
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	log := filepath.Join(cwd, "protocol.jsonl")
	if err := os.WriteFile(filepath.Join(cwd, "example.ts"), []byte("🙂 value\nexport const value = 1;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tool := &LSPTool{Configs: []lsp.ServerConfig{{Name: "fixture", Command: node, Args: []string{fixture, log}, Languages: map[string]string{".ts": "typescript"}, InitializationOptions: options, Settings: map[string]any{"language": map[string]any{"example": "configured"}}, TimeoutMs: 4000}}}
	t.Cleanup(func() { _ = tool.Dispose() })
	return tool, WithExecutionContext(context.Background(), ExecutionContext{WorkDir: cwd}), log
}
func lspArgs(operation string) map[string]any {
	return map[string]any{"operation": operation, "file_path": "example.ts", "line": 1, "character": 3}
}
func lspMessages(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	return messages
}
func TestLSPProtocolAndLifecycle(t *testing.T) {
	tool, ctx, log := newTestLSP(t, nil)
	for _, operation := range LSPOperations {
		args := lspArgs(operation)
		if operation == "workspaceSymbol" {
			args["query"] = "value"
		}
		result := tool.Execute(ctx, args)
		if result.IsError {
			t.Fatalf("%s: %s", operation, result.Output)
		}
		if operation == "hover" && (!strings.Contains(result.Output, `"character": 2`) || !strings.Contains(result.Output, "🙂 value")) {
			t.Fatalf("UTF-16 coordinate or text lost: %s", result.Output)
		}
		if operation == "diagnostics" && (!strings.Contains(result.Output, "current diagnostic🙂") || strings.Contains(result.Output, "stale")) {
			t.Fatalf("bad diagnostics: %s", result.Output)
		}
		if strings.HasSuffix(operation, "Calls") && !strings.Contains(result.Output, "preserved") {
			t.Fatal(result.Output)
		}
	}
	if err := os.WriteFile(filepath.Join(workingDirectory(ctx), "example.ts"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result := tool.Execute(ctx, lspArgs("hover"))
	if result.IsError || !strings.Contains(result.Output, `"version": 2`) {
		t.Fatal(result)
	}
	_ = tool.Dispose()
	counts := map[string]int{}
	editDenied, configReturned := false, false
	for _, message := range lspMessages(t, log) {
		method, _ := message["method"].(string)
		counts[method]++
		if message["id"] == "server-edit" {
			result, _ := message["result"].(map[string]any)
			editDenied = result["applied"] == false
		}
		if message["id"] == "server-config" {
			result, _ := message["result"].([]any)
			configReturned = len(result) == 1 && result[0] == "configured"
		}
	}
	for _, method := range []string{"initialize", "textDocument/didOpen", "textDocument/didChange", "shutdown", "exit"} {
		if counts[method] != 1 {
			t.Fatalf("%s: %d", method, counts[method])
		}
	}
	if !editDenied || !configReturned {
		t.Fatal("server requests were not handled correctly")
	}
	if result := tool.Execute(ctx, lspArgs("hover")); !result.IsError || !strings.Contains(result.Output, "disposed") {
		t.Fatal(result)
	}
}
func TestLSPConcurrentInitializationCancellationAndRestart(t *testing.T) {
	tool, ctx, log := newTestLSP(t, map[string]any{"delay": 50})
	cancelled, cancel := context.WithCancel(ctx)
	first := make(chan ToolResult, 1)
	go func() { first <- tool.Execute(cancelled, lspArgs("hover")) }()
	time.AfterFunc(10*time.Millisecond, cancel)
	var wg sync.WaitGroup
	results := make(chan ToolResult, 3)
	for _, operation := range []string{"hover", "definition", "references"} {
		wg.Add(1)
		go func() { defer wg.Done(); results <- tool.Execute(ctx, lspArgs(operation)) }()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.IsError {
			t.Fatal(result.Output)
		}
	}
	if result := <-first; !result.IsError {
		t.Fatal("cancelled initialization succeeded")
	}
	hung, cancelHang := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelHang()
	args := map[string]any{"operation": "workspaceSymbol", "file_path": "example.ts", "query": "hang"}
	if result := tool.Execute(hung, args); !result.IsError {
		t.Fatal(result)
	}
	for _, query := range []string{"bad-frame", "crash"} {
		args["query"] = query
		result := tool.Execute(ctx, args)
		if !result.IsError {
			t.Fatal(result)
		}
		if query == "bad-frame" && !strings.Contains(result.Output, "exceeds 16MB") {
			t.Fatal(result.Output)
		}
		var restarted sync.WaitGroup
		errors := make(chan string, 3)
		for range 3 {
			restarted.Add(1)
			go func() {
				defer restarted.Done()
				if result := tool.Execute(ctx, lspArgs("hover")); result.IsError {
					errors <- result.Output
				}
			}()
		}
		restarted.Wait()
		close(errors)
		for err := range errors {
			t.Fatal(err)
		}
	}
	_ = tool.Dispose()
	initialized := 0
	cancelledRequest := false
	for _, message := range lspMessages(t, log) {
		if message["method"] == "initialize" {
			initialized++
		}
		if message["method"] == "$/cancelRequest" {
			cancelledRequest = true
		}
	}
	if initialized != 3 || !cancelledRequest {
		t.Fatalf("initialize=%d cancellation=%t", initialized, cancelledRequest)
	}
}
func TestLSPBoundsDiagnosticsAndEncoding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options map[string]any
		want    string
		isError bool
	}{
		{"large diagnostic", map[string]any{"largeDiagnostics": true}, `"truncated": true`, false},
		{"pull", map[string]any{"pull": true}, "pull diagnostic", false},
		{"encoding", map[string]any{"encoding": "utf-8"}, "Only UTF-16", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool, ctx, _ := newTestLSP(t, tc.options)
			result := tool.Execute(ctx, map[string]any{"operation": "diagnostics", "file_path": "example.ts"})
			if result.IsError != tc.isError || !strings.Contains(result.Output, tc.want) {
				t.Fatal(result)
			}
		})
	}
	tool, ctx, log := newTestLSP(t, map[string]any{"silent": true})
	for _, args := range []map[string]any{{"operation": "hover", "file_path": "example.ts"}, {"operation": "hover", "file_path": "example.ts", "line": 1.5, "character": 1}, {"operation": "workspaceSymbol", "file_path": "example.ts", "query": " "}} {
		if result := tool.Execute(ctx, args); !result.IsError {
			t.Fatal(result)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("invalid arguments launched a server")
	}
	args := lspArgs("hover")
	args["character"] = 100
	if result := tool.Execute(ctx, args); !result.IsError || !strings.Contains(result.Output, "outside") {
		t.Fatal(result)
	}
	text := strings.Repeat("界", 1500000)
	for _, name := range []string{"first.ts", "second.ts"} {
		if err := os.WriteFile(filepath.Join(workingDirectory(ctx), name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		result := tool.Execute(ctx, map[string]any{"operation": "documentSymbol", "file_path": name})
		if result.IsError {
			t.Fatal(result)
		}
	}
	closed := false
	for _, message := range lspMessages(t, log) {
		if message["method"] == "textDocument/didClose" {
			closed = true
		}
	}
	if !closed {
		t.Fatal("byte budget did not evict document")
	}
}

func TestLSPCancellationWhenServerStopsReading(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the LSP fixture")
	}
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "example.ts"), []byte(strings.Repeat("a", 1024*1024)), 0600); err != nil {
		t.Fatal(err)
	}
	script := `process.stdin.once('data', () => { process.stdin.pause(); const body=JSON.stringify({jsonrpc:'2.0',id:1,result:{capabilities:{}}}); process.stdout.write('Content-Length: '+Buffer.byteLength(body)+'\r\n\r\n'+body); }); setInterval(()=>{}, 1000);`
	tool := &LSPTool{Configs: []lsp.ServerConfig{{Name: "stalled", Command: node, Args: []string{"-e", script}, Languages: map[string]string{".ts": "typescript"}, TimeoutMs: 4000}}}
	defer tool.Dispose()
	ctx := WithExecutionContext(context.Background(), ExecutionContext{WorkDir: cwd})
	if _, err := tool.session(ctx, tool.Configs[0], cwd, true); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	done := make(chan ToolResult, 1)
	go func() { done <- tool.Execute(cancelled, lspArgs("hover")) }()
	select {
	case result := <-done:
		if !result.IsError {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		_ = tool.Dispose()
		t.Fatal("blocked stdin prevented cancellation")
	}
}
