package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yukino-js/yukino/yukino/internal/bootstrap"
	"github.com/yukino-js/yukino/yukino/internal/config"
	"github.com/yukino-js/yukino/yukino/internal/mcp"
)

func (m *Model) mcpStatus() string {
	if m.mcpMgr == nil {
		return "No MCP servers configured."
	}
	connected := m.mcpMgr.ConnectedServers()
	lines := []string{"No MCP servers connected."}
	if len(connected) > 0 {
		lines = []string{fmt.Sprintf("MCP servers (%d):", len(connected))}
		for _, server := range connected {
			lines = append(lines, "  · "+server)
		}
		lines = append(lines, fmt.Sprintf("Tools: %d total", bootstrap.CountMcpTools(m.registry)))
	}
	if missing := m.mcpMgr.MissingServers(m.mcpConfigs); len(missing) > 0 {
		lines = append(lines, "Not connected: "+strings.Join(missing, ", "))
	}
	return strings.Join(lines, "\n")
}

func (m Model) runMcpCommand(args string) (tea.Model, tea.Cmd) {
	reload := strings.EqualFold(strings.TrimSpace(args), "reload")
	if reload {
		cfg, err := config.LoadConfig("", config.LoadOptions{})
		if err != nil {
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "MCP reload failed: " + err.Error()})
			return m, nil
		}
		m.mcpConfigs = config.WithProjectMcpServers(cfg, m.getWorkDir()).MCPServers
		if len(m.mcpConfigs) == 0 {
			if m.mcpMgr != nil {
				m.mcpMgr.DisconnectAll()
				mcp.SyncMcpInstructions(m.conversation, m.mcpAnnounced, m.mcpMgr)
			}
			bootstrap.RemoveMcpTools(m.registry)
			m.mcpMgr = nil
			m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "MCP config reloaded: no MCP servers are configured."})
			return m, nil
		}
		mcp.RetractMcpInstructions(m.conversation, m.mcpAnnounced)
		names := make([]string, 0, len(m.mcpConfigs))
		for _, server := range m.mcpConfigs {
			names = append(names, server.Name)
		}
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "Reloading MCP server(s): " + strings.Join(names, ", ")})
		bootstrap.RemoveMcpTools(m.registry)
	} else if m.mcpMgr == nil {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "No MCP servers configured."})
		return m, nil
	} else if missing := m.mcpMgr.MissingServers(m.mcpConfigs); len(missing) == 0 {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: m.mcpStatus()})
		return m, nil
	} else {
		m.chatMessages = append(m.chatMessages, chatMessage{role: "system", content: "Connecting MCP server(s): " + strings.Join(missing, ", ")})
	}
	if m.mcpMgr == nil {
		m.mcpMgr = mcp.NewManager(false)
	}
	mgr, configs, ctx := m.mcpMgr, m.mcpConfigs, m.lifeCtx
	return m, func() tea.Msg {
		if reload {
			return mcpReadyMsg{mgr: mgr, result: mgr.ReconnectAll(ctx, configs), reloaded: true}
		}
		return mcpReadyMsg{mgr: mgr, result: mgr.ConnectAll(ctx, configs), showStatus: !reload, reloaded: reload}
	}
}
