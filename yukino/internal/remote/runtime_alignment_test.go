package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yukino-js/yukino/yukino/internal/agent"
	"github.com/yukino-js/yukino/yukino/internal/commands"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/conversation"
	"github.com/yukino-js/yukino/yukino/internal/history"
	"github.com/yukino-js/yukino/yukino/internal/llm"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
	"github.com/yukino-js/yukino/yukino/internal/permissions"
	"github.com/yukino-js/yukino/yukino/internal/session"
	"github.com/yukino-js/yukino/yukino/internal/skills"
	"github.com/yukino-js/yukino/yukino/internal/tools"
)

func TestInboundValidationMatchesReferenceSchemas(t *testing.T) {
	data, err := os.ReadFile("testdata/m44-ws-validation.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string
		Kind  string
		Data  json.RawMessage
		Valid bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			raw, err := json.Marshal(clientMessage{Type: test.Kind, Data: test.Data})
			if err != nil {
				t.Fatal(err)
			}
			if _, got := parseClientMessage(raw); got != test.Valid {
				t.Fatalf("valid=%v, reference=%v, data=%s", got, test.Valid, test.Data)
			}
		})
	}
}

func TestUploadBase64MatchesNode(t *testing.T) {
	data, err := os.ReadFile("testdata/m44-base64.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Input, Output string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		if got := base64.StdEncoding.EncodeToString(decodeUploadBase64(test.Input)); got != test.Output {
			t.Fatalf("input=%q, got=%q, reference=%q", test.Input, got, test.Output)
		}
	}
}

func TestLazyProviderActivationBuildsFullRuntime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	s := NewServer([]config.ProviderConfig{{Name: "p", Protocol: "openai", APIKey: "test", BaseURL: "https://example.com", Model: "m"}}, nil, nil, "", false, 0, nil, false)
	t.Cleanup(s.Stop)
	s.applyProviderSwitch(0)
	if s.ag == nil || s.client == nil || s.sessionID == "" || s.registry == nil {
		t.Fatal("provider switch did not initialize the runtime")
	}
	if s.cmdRegistry.Find("provider") == nil || s.registry.Get("LoadSkill") == nil || s.registry.Get("InstallSkill") != nil {
		t.Fatal("remote registrations differ from reference")
	}
}

type alignmentClient struct {
	calls   atomic.Int32
	started chan []conversation.Message
	release <-chan struct{}
	observe func()
}

func (c *alignmentClient) SetSystemPrompt(string) {}

func (c *alignmentClient) Stream(ctx context.Context, conv *conversation.Manager, _ []map[string]any) (<-chan llm.StreamEvent, <-chan error) {
	c.calls.Add(1)
	if c.observe != nil {
		c.observe()
	}
	events, errs := make(chan llm.StreamEvent, 2), make(chan error)
	if c.started != nil {
		c.started <- slices.Clone(conv.GetMessages())
	}
	go func() {
		defer close(events)
		defer close(errs)
		if c.release != nil {
			select {
			case <-c.release:
			case <-ctx.Done():
				return
			}
		}
		events <- llm.TextDelta{Text: "done"}
		events <- llm.StreamEnd{StopReason: "end_turn", Usage: llm.UsageInfo{InputTokens: 7, OutputTokens: 3}}
	}()
	return events, errs
}

func TestRemoteRunsUseFreshAgentAndSharedSessionState(t *testing.T) {
	client := &alignmentClient{}
	s := alignmentRuntime(t, client)
	s.ActivateSkill("demo", "skill body")
	var running []*agent.Agent
	client.observe = func() {
		s.runMu.Lock()
		defer s.runMu.Unlock()
		running = append(running, s.currentAgent)
	}
	s.handleUserMessage("first", nil)
	s.handleUserMessage("second", nil)
	if len(running) != 2 || running[0] == running[1] || running[0] == s.ag {
		t.Fatalf("runtime instances: %v", running)
	}
	for _, instance := range running {
		if instance.RecoveryState != s.ag.RecoveryState || instance.FileStateCache != s.ag.FileStateCache || instance.GetActiveSkills()["demo"] != "skill body" {
			t.Fatal("per-session state was not inherited by the new runtime")
		}
	}
}

func TestConcurrentAskRequestsRemainIndependentlyAnswerable(t *testing.T) {
	s := newPendingServer()
	s.askUserCh = make(chan tools.AskUserRequest, 2)
	done := make(chan struct{})
	defer close(done)
	go s.listenForAskUser(done)
	first, second := make(chan tools.QuestionResponse, 1), make(chan tools.QuestionResponse, 1)
	s.askUserCh <- tools.AskUserRequest{Context: context.Background(), Questions: []tools.Question{{Text: "first"}}, ResponseCh: first}
	s.askUserCh <- tools.AskUserRequest{Context: context.Background(), Questions: []tools.Question{{Text: "second"}}, ResponseCh: second}
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.pendingAskMu.Lock()
		ready := len(s.pendingAsks) == 2
		s.pendingAskMu.Unlock()
		if ready {
			break
		}
		select {
		case <-deadline:
			s.cancelActiveRun()
			t.Fatal("second question waited for the first answer")
		case <-ticker.C:
		}
	}
	s.pendingAskMu.Lock()
	ids := make([]string, 0, 2)
	for id := range s.pendingAsks {
		ids = append(ids, id)
	}
	s.pendingAskMu.Unlock()
	for _, id := range ids {
		s.handleAskUserResponse(askUserResponseData{ID: id, Answers: map[string]string{"answer": "yes"}})
	}
	for _, response := range []<-chan tools.QuestionResponse{first, second} {
		select {
		case value := <-response:
			if value.Answers["answer"] != "yes" {
				t.Fatal(value)
			}
		case <-time.After(time.Second):
			t.Fatal("question was not answered")
		}
	}
}

func TestMCPReloadRemovesToolsWhenConfigBecomesEmpty(t *testing.T) {
	s := alignmentRuntime(t, &alignmentClient{})
	path := config.GlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("providers:\n  - name: p\n    protocol: openai\n    base_url: https://example.com\n    api_key: test\n    model: m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tool := mcp.NewMCPToolWrapper(nil, "old", mcp.MCPTool{Name: "tool"})
	s.registry.Register(tool)
	s.mcpMgr = mcp.NewManager(true)
	s.mcpAnnounced["old"] = true
	client := dialClient(t, s)
	s.reloadMcpServers(context.Background())
	message := readJSON(t, client)
	if message["type"] != "system" || message["data"].(map[string]any)["message"] != "MCP config reloaded: no MCP servers are configured." {
		t.Fatal(message)
	}
	if s.registry.Get(tool.Name()) != nil || s.mcpMgr != nil || len(s.mcpAnnounced) != 0 {
		t.Fatal("MCP reload retained stale server state")
	}
}

func TestForkSkillRunsOutsideMainConversation(t *testing.T) {
	client := &alignmentClient{started: make(chan []conversation.Message, 1)}
	s := alignmentRuntime(t, client)
	dir := filepath.Join(mustGetwd(), ".agents", "skills", "forked")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: forked\ndescription: isolated task\nmode: fork\nfork_context: recent\n---\nSecret skill body\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.skillCatalog = skills.LoadCatalog(mustGetwd())
	s.wireSkillCommands()
	s.conv.AddUserMessage("parent context")
	s.handleUserMessage("/forked task", nil)
	if client.calls.Load() != 1 {
		t.Fatalf("fork did not run: %d", client.calls.Load())
	}
	for _, message := range s.conv.GetMessages() {
		if strings.Contains(message.Content, "Secret skill body") {
			t.Fatal("fork prompt leaked into main conversation")
		}
	}
	if s.conv.Len() != 1 {
		t.Fatalf("fork changed main history: %#v", s.conv.GetMessages())
	}
	seen := <-client.started
	found := false
	for _, message := range seen {
		if strings.Contains(message.Content, "Secret skill body") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fork did not receive skill prompt: %#v", seen)
	}
}

func alignmentRuntime(t *testing.T, client llm.Client) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	s := NewServer([]config.ProviderConfig{{Name: "p", Protocol: "openai", Model: "m"}}, nil, nil, "", false, 0, nil, false)
	s.client = client
	s.conv = conversation.NewManager()
	s.registry = tools.NewRegistry()
	s.sessionID = session.NewID()
	s.cmdRegistry = commands.CreateDefaultRegistry()
	s.ag = agent.New(client, s.registry, "openai")
	s.ag.Checker = permissions.NewChecker(permissions.NewPathSandbox(mustGetwd(), true), permissions.NewRuleEngine(mustGetwd()), permissions.ModeDefault)
	s.ag.SetSessionID(s.sessionID)
	s.ag.ContextWindow = 200000
	return s
}

func TestPromptCommandStoresDisplayTextAndRunsExpandedPrompt(t *testing.T) {
	client := &alignmentClient{started: make(chan []conversation.Message, 2)}
	s := alignmentRuntime(t, client)
	s.cmdRegistry.Register(&commands.Command{Name: "demo", Type: commands.TypePrompt, Handler: func(ctx *commands.Context) string { return "expanded " + ctx.Args }})
	s.handleUserMessage("/demo task", nil)
	seen := <-client.started
	found := false
	for _, message := range seen {
		if message.Role == "user" && message.Content == "expanded task" {
			found = true
		}
	}
	if !found {
		t.Fatalf("model did not receive expanded prompt: %#v", seen)
	}
	saved, err := session.LoadSession(mustGetwd(), s.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 || saved[0].Content != "/demo task" {
		t.Fatalf("stored transcript: %#v", saved)
	}
	inputs, err := history.LoadInputs()
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].CommandName == nil || *inputs[0].CommandName != "demo" {
		t.Fatalf("input metadata: %#v", inputs)
	}
	if s.isStreaming() {
		t.Fatal("command did not release its run slot")
	}
}

func TestBusyLocalCommandQueuesInputUntilRunSlotIsReleased(t *testing.T) {
	client := &alignmentClient{}
	s := alignmentRuntime(t, client)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.cmdRegistry.Register(&commands.Command{Name: "slow", Type: commands.TypeLocal, Handler: func(*commands.Context) string { close(entered); <-release; return "ready" }})
	go func() { defer close(done); s.handleUserMessage("/slow", nil) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("command did not start")
	}
	if _, ok := s.beginRun(); ok {
		t.Fatal("local command allowed a concurrent run")
	}
	s.handleSteeringMessage("/help", nil)
	s.handleSteeringMessage("next task", nil)
	s.followUpsMu.Lock()
	queued := len(s.followUps)
	s.followUpsMu.Unlock()
	if queued != 2 {
		t.Fatalf("follow-ups=%d", queued)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("follow-ups did not settle")
	}
	if client.calls.Load() != 1 {
		t.Fatalf("model calls=%d", client.calls.Load())
	}
	inputs, err := history.LoadInputs()
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3 {
		t.Fatalf("input was lost or recorded twice: %#v", inputs)
	}
}

func TestCancelDropsUndeliveredSteering(t *testing.T) {
	client := &alignmentClient{started: make(chan []conversation.Message, 1), release: make(chan struct{})}
	s := alignmentRuntime(t, client)
	done := make(chan struct{})
	go func() { defer close(done); s.handleUserMessage("first task", nil) }()
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not start")
	}
	s.handleSteeringMessage("do not replay after cancel", nil)
	s.cancelActiveRun()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled run did not settle")
	}
	if client.calls.Load() != 1 {
		t.Fatalf("canceled steering started another run: %d", client.calls.Load())
	}
	if len(s.steeringLeftovers) != 0 || s.currentAgent != nil || s.isStreaming() {
		t.Fatal("canceled run retained active state")
	}
	for _, message := range s.conv.GetMessages() {
		if message.Content == "do not replay after cancel" {
			t.Fatal("undelivered steering entered the conversation")
		}
	}
}

func TestRapidMessagesClaimRunSlotBeforeStartingGoroutine(t *testing.T) {
	release := make(chan struct{})
	client := &alignmentClient{started: make(chan []conversation.Message, 2), release: release}
	s := alignmentRuntime(t, client)
	s.submitUserMessage("first")
	s.submitUserMessage("second")
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not start")
	}
	close(release)
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for client.calls.Load() != 2 || s.isStreaming() {
		select {
		case <-deadline:
			t.Fatalf("rapid input was dropped: model calls=%d", client.calls.Load())
		case <-ticker.C:
		}
	}
	inputs, err := history.LoadInputs()
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("input metadata=%v", inputs)
	}
}

func TestEventBridgeFlushesTextAndIncludesUsageAndStopReason(t *testing.T) {
	s := alignmentRuntime(t, &alignmentClient{})
	client := dialClient(t, s)
	events := make(chan agent.AgentEvent, 5)
	events <- agent.StreamText{Text: "partial"}
	events <- agent.ToolResultEvent{ToolID: "id", ToolName: "ReadFile", Output: "ok"}
	events <- agent.TurnComplete{Turn: 77}
	events <- agent.UsageEvent{InputTokens: 11, OutputTokens: 22}
	events <- agent.LoopComplete{TotalTurns: 99, StopReason: "interrupted"}
	close(events)
	s.agentCh = events
	s.consumeAgentEvents()
	want := []string{"stream_text", "stream_end", "tool_result", "turn_complete", "usage", "loop_complete"}
	for _, kind := range want {
		message := readJSON(t, client)
		if message["type"] != kind {
			t.Fatalf("event=%v, want %s", message, kind)
		}
		data := message["data"].(map[string]any)
		switch kind {
		case "turn_complete":
			if data["turn"] != float64(1) {
				t.Fatalf("turn=%v", data)
			}
		case "usage":
			if data["contextWindow"] != float64(200000) || data["inputTokens"] != float64(11) {
				t.Fatalf("usage=%v", data)
			}
		case "loop_complete":
			if data["stopReason"] != "interrupted" || data["totalTurns"] != float64(1) {
				t.Fatalf("completion=%v", data)
			}
		}
	}
}

func TestRemoteStartsWithoutProviderAndReportsActualEphemeralPort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := NewServer(nil, nil, nil, "127.0.0.1:0", false, 0, nil, false)
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	t.Cleanup(s.Stop)
	var address string
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for address == "" {
		select {
		case err := <-done:
			t.Fatalf("server exited before listening: %v", err)
		case <-deadline:
			t.Fatal("server did not listen")
		case <-ticker.C:
			s.runMu.Lock()
			if s.httpServer != nil {
				address = s.httpServer.Addr
			}
			s.runMu.Unlock()
		}
	}
	if strings.HasSuffix(address, ":0") {
		t.Fatalf("ephemeral address=%s", address)
	}
	response, err := http.Get("http://" + address + "/health")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]any
	err = json.NewDecoder(response.Body).Decode(&health)
	response.Body.Close()
	if err != nil || health["status"] != "ok" || health["remote"] != true {
		t.Fatalf("health=%v, err=%v", health, err)
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws?token="+s.accessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, kind := range []string{"commands", "history", "provider_list"} {
		if message := readJSON(t, ws); message["type"] != kind {
			t.Fatalf("bootstrap=%v, want %s", message, kind)
		}
	}
	if err := ws.WriteJSON(wsMessage{Type: "user_message", Data: map[string]string{"content": "/login"}}); err != nil {
		t.Fatal(err)
	}
	for {
		message := readJSON(t, ws)
		if message["type"] == "error" {
			t.Fatalf("login required an initialized provider: %v", message)
		}
		if message["type"] == "run_state" && message["data"].(map[string]any)["streaming"] == false {
			break
		}
	}
	s.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after teardown")
	}
}
