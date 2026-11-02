package agent

import (
	"context"
	"sync"

	"github.com/yukino-js/yukino/yukino/internal/telemetry"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

type toolBatch struct {
	concurrent bool
	calls      []toolCallEntry
}

type toolCallEntry struct {
	tc    toolCallInfo
	index int
}

type toolCallInfo struct {
	toolID     string
	toolName   string
	arguments  map[string]any
	parseError string
}

type StreamingExecutor struct {
	registry  *tools.Registry
	eventCh   chan AgentEvent
	telemetry *telemetry.AgentTelemetry

	mu      sync.Mutex
	calls   []toolCallInfo
	results []toolExecResult
}

func NewStreamingExecutor(registry *tools.Registry, eventCh chan AgentEvent, agentTelemetry *telemetry.AgentTelemetry) *StreamingExecutor {
	return &StreamingExecutor{
		registry:  registry,
		eventCh:   eventCh,
		telemetry: agentTelemetry,
	}
}

func (se *StreamingExecutor) Submit(tc toolCallInfo) {
	se.mu.Lock()
	se.calls = append(se.calls, tc)
	se.mu.Unlock()
}

func (se *StreamingExecutor) ExecuteAll(ctx context.Context, agent *Agent) []toolExecResult {
	se.mu.Lock()
	calls := append([]toolCallInfo(nil), se.calls...)
	se.mu.Unlock()

	results := make([]toolExecResult, len(calls))

	var entries []toolCallEntry
	for i, c := range calls {
		entries = append(entries, toolCallEntry{tc: c, index: i})
	}

	batches := partitionToolCalls(entries, se.registry)

	for _, batch := range batches {
		if batch.concurrent && len(batch.calls) > 1 {
			var wg sync.WaitGroup
			for _, entry := range batch.calls {
				wg.Add(1)
				go func(e toolCallEntry) {
					defer wg.Done()
					r := se.observeTool(ctx, agent, e.tc)
					se.emitResult(r)
					se.mu.Lock()
					results[e.index] = r
					se.mu.Unlock()
				}(entry)
			}
			wg.Wait()
		} else {
			for _, entry := range batch.calls {
				r := se.observeTool(ctx, agent, entry.tc)
				se.emitResult(r)
				results[entry.index] = r
			}
		}
	}

	se.results = results
	return results
}

// observeTool wraps a single tool execution with the tool observation,
// mirroring observeToolExecution around tool.execute in the reference.
func (se *StreamingExecutor) observeTool(ctx context.Context, agent *Agent, tc toolCallInfo) toolExecResult {
	return telemetry.ObserveToolExecution(tc.toolName, func() toolExecResult {
		return agent.executeSingleTool(ctx, se.eventCh, tc)
	}, func(result toolExecResult) bool { return result.isError }, se.telemetry)
}

func (se *StreamingExecutor) emitResult(r toolExecResult) {
	se.eventCh <- ToolResultEvent{ToolID: r.toolID, ToolName: r.toolName, Output: r.output, IsError: r.isError, Elapsed: r.elapsed, ContentBlocks: r.contentBlocks}
}

func partitionToolCalls(entries []toolCallEntry, registry *tools.Registry) []toolBatch {
	var batches []toolBatch
	for _, entry := range entries {
		tool := registry.Get(entry.tc.toolName)
		safe := tool != nil && tools.IsConcurrencySafe(tool, entry.tc.arguments)

		if safe && len(batches) > 0 && batches[len(batches)-1].concurrent {
			batches[len(batches)-1].calls = append(batches[len(batches)-1].calls, entry)
		} else {
			batches = append(batches, toolBatch{
				concurrent: safe,
				calls:      []toolCallEntry{entry},
			})
		}
	}
	return batches
}

func (se *StreamingExecutor) HasPending() bool {
	se.mu.Lock()
	defer se.mu.Unlock()
	return len(se.calls) > 0
}

func (se *StreamingExecutor) Reset() {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.calls = nil
	se.results = nil
}
