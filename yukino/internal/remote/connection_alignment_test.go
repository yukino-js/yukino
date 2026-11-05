package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yukino-js/yukino/yukino/internal/agent"
)

func connectRemote(t *testing.T, s *Server, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http")+"/ws?token="+s.accessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	for {
		message := readJSON(t, conn)
		if message["type"] == "provider_list" {
			return conn
		}
	}
}

func TestRemoteReconnectReplaysPendingRequestAndRejectsMalformedReply(t *testing.T) {
	s := newPendingServer()
	server := httptest.NewServer(http.HandlerFunc(s.handleWS))
	t.Cleanup(server.Close)
	first := connectRemote(t, s, server.URL)
	answer := make(chan agent.PermissionReply, 1)
	s.requestPermission(context.Background(), permissionRequestPayload{ToolName: "Bash", Description: "run", RememberPatterns: []string{"Bash(echo *)"}}, answer)
	t.Cleanup(s.cancelActiveRun)
	request := readJSON(t, first)
	if request["type"] != "permission_request" {
		t.Fatal(request)
	}
	id := request["data"].(map[string]any)["id"].(string)
	second := connectRemote(t, s, server.URL)
	replay := readJSON(t, second)
	if replay["type"] != "permission_request" || replay["data"].(map[string]any)["id"] != id {
		t.Fatal("reconnect lost the pending request", replay)
	}
	if err := first.WriteJSON(map[string]any{"type": "permission_response", "data": map[string]any{"id": id, "response": "invalid"}}); err != nil {
		t.Fatal(err)
	}
	if err := first.WriteJSON(map[string]any{"type": "ping", "data": nil}); err != nil {
		t.Fatal(err)
	}
	if pong := readJSON(t, first); pong["type"] != "pong" {
		t.Fatal("malformed reply claimed the request", pong)
	}
	select {
	case reply := <-answer:
		t.Fatal("malformed reply resolved the request", reply)
	default:
	}
	if err := second.WriteJSON(map[string]any{"type": "permission_response", "data": map[string]any{"id": id, "response": "allowAlways"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-answer:
		if reply.Response != agent.PermAllowAlways {
			t.Fatal(reply)
		}
	case <-time.After(time.Second):
		t.Fatal("valid response did not resolve the request")
	}
	for _, conn := range []*websocket.Conn{first, second} {
		if message := readJSON(t, conn); message["type"] != "request_expired" {
			t.Fatal("resolution was not broadcast exactly once", message)
		}
	}
}

func TestRemotePingRepliesOnlyToSender(t *testing.T) {
	s := newPendingServer()
	server := httptest.NewServer(http.HandlerFunc(s.handleWS))
	t.Cleanup(server.Close)
	first, second := connectRemote(t, s, server.URL), connectRemote(t, s, server.URL)
	if err := first.WriteJSON(map[string]any{"type": "ping", "data": nil}); err != nil {
		t.Fatal(err)
	}
	if message := readJSON(t, first); message["type"] != "pong" {
		t.Fatal(message)
	}
	second.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, _, err := second.ReadMessage(); err == nil {
		t.Fatal("ping was broadcast to another client")
	}
}

func TestRemoteRunOwnershipAndCanceledSteering(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newPendingServer()
	s.ag = &agent.Agent{}
	s.steeringLeftovers = []agent.SteeringMessage{{Text: "late input"}}
	ctx, claimed := s.beginRun()
	if !claimed {
		t.Fatal("first run did not acquire the slot")
	}
	if _, claimed := s.beginRun(); claimed {
		t.Fatal("concurrent run acquired an occupied slot")
	}
	s.requestPlanApproval()
	s.cancelActiveRun()
	if ctx.Err() == nil || s.planApprovalID != "" {
		t.Fatal("cancel retained active work or approval")
	}
	s.finishRun()
	if len(s.steeringLeftovers) != 0 {
		t.Fatal("canceled steering was retained", s.steeringLeftovers)
	}
	s.stopping = true
	if _, claimed := s.beginRun(); claimed {
		t.Fatal("stopped server accepted a new run")
	}
}
