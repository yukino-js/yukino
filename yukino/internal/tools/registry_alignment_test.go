package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

type disposableTool struct {
	mockNormalTool
	disposed atomic.Int32
}

func (t *disposableTool) Dispose() error { t.disposed.Add(1); return nil }
func TestRegistryOwnershipReloadAndProtocolContracts(t *testing.T) {
	root := NewRegistry()
	owned := &disposableTool{mockNormalTool: mockNormalTool{name: "ReadFile"}}
	root.Register(owned)
	child := NewRegistry()
	child.RegisterBorrowed(owned)
	if err := child.Dispose(); err != nil {
		t.Fatal(err)
	}
	if owned.disposed.Load() != 0 {
		t.Fatal("borrower disposed parent tool")
	}
	if err := root.Dispose(); err != nil {
		t.Fatal(err)
	}
	if err := root.Dispose(); err != nil {
		t.Fatal(err)
	}
	if owned.disposed.Load() != 1 {
		t.Fatal("owned tool not disposed once")
	}
	reg := NewRegistry()
	reg.Register(&mockNormalTool{name: "Z"})
	reg.Register(&mockNormalTool{name: "A"})
	if got := reg.GetAllSchemas("anthropic"); SchemaName(got[0]) != "Z" || got[0]["type"] != "custom" {
		t.Fatal(got)
	}
	compat := reg.GetAllSchemas("openai-compat")
	function, _ := compat[0]["function"].(map[string]any)
	if function == nil || function["name"] != "Z" || function["strict"] != false {
		t.Fatal(compat)
	}
	deferred := &mockDeferredTool{name: "mcp__test__query"}
	reg.Register(deferred)
	reg.MarkDiscovered(deferred.Name())
	reg.Unregister(deferred.Name())
	reg.Register(deferred)
	if reg.IsDiscovered(deferred.Name()) {
		t.Fatal("reloaded tool kept discovery state")
	}
	clone := NewRegistry()
	clone.CopyLoadingStateFrom(reg)
	reg.MarkDiscovered(deferred.Name())
	if clone.IsDiscovered(deferred.Name()) {
		t.Fatal("discovery state is shared")
	}
}
func TestMcpCallRejectsBuiltinMissingArgumentsAndMismatchedServer(t *testing.T) {
	reg, dispatcher, tool := newDispatchRegistry()
	reg.Register(&mockNormalTool{name: "ReadFile"})
	for _, args := range []map[string]any{
		{"server": "linear", "tool": "ReadFile", "arguments": map[string]any{}},
		{"server": "typo", "tool": tool.Name(), "arguments": map[string]any{}},
		{"server": "linear", "tool": tool.Name()},
	} {
		if result := dispatcher.Execute(context.Background(), args); !result.IsError {
			t.Fatal("invalid dispatch accepted", args)
		}
	}
	if tool.received != nil {
		t.Fatal("invalid dispatch executed target")
	}
	if dispatcher.Name() != "McpCall" {
		t.Fatal("obsolete dispatch name")
	}
	result := dispatcher.Execute(context.Background(), map[string]any{"server": "linear", "tool": tool.Name()})
	if !strings.Contains(result.Output, "arguments") {
		t.Fatal(result)
	}
}
