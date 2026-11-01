package mcp

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

const (
	DefaultEagerThresholdPercent = 10

	CharsPerToken = 2.5

	NativeToolSearchBeta = "advanced-tool-use-2025-11-20"

	envLoadingOverride = "YUKINO_MCP_LOADING"
)

var officialHosts = map[string]bool{"api.anthropic.com": true}

func IsOfficialAnthropicEndpoint(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return officialHosts[strings.ToLower(u.Hostname())]
}

func EstimateSchemaTokens(schemaChars int) int {
	return int(float64(schemaChars) / CharsPerToken)
}

func DecideMode(baseURL string, contextWindow, mcpSchemaChars, thresholdPercent int) tools.McpLoadingMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envLoadingOverride))) {
	case "eager":
		return tools.McpLoadingEager
	case "native":
		return tools.McpLoadingNative
	case "dispatch":
		return tools.McpLoadingDispatch
	}

	if mcpSchemaChars <= 0 {
		return tools.McpLoadingEager
	}

	budget := float64(contextWindow) * float64(thresholdPercent) / 100
	if float64(EstimateSchemaTokens(mcpSchemaChars)) < budget {
		return tools.McpLoadingEager
	}
	if IsOfficialAnthropicEndpoint(baseURL) {
		return tools.McpLoadingNative
	}
	return tools.McpLoadingDispatch
}

func MeasureSchemaChars(registry *tools.Registry) int {
	total := 0
	for _, t := range registry.ListTools() {
		if !strings.HasPrefix(t.Name(), tools.MCPToolPrefix) {
			continue
		}
		if b, err := json.Marshal(t.Schema()); err == nil {
			total += len(b)
		} else {
			total += len(t.Name()) + len(t.Description())
		}
	}
	return total
}

func ApplyMode(registry *tools.Registry, mode tools.McpLoadingMode) {
	registry.McpLoadingMode = mode
	eager := mode == tools.McpLoadingEager
	for _, t := range registry.ListTools() {
		if mt, ok := t.(tools.MCPTool); ok {
			mt.SetDeferLoading(!eager)
		}
	}

	registry.ExposeToolSearch = !eager
	registry.ExposeMcpCall = mode == tools.McpLoadingDispatch
}

func DecideAndApply(registry *tools.Registry, baseURL string, contextWindow int) tools.McpLoadingMode {
	mode := DecideMode(baseURL, contextWindow, MeasureSchemaChars(registry), DefaultEagerThresholdPercent)
	ApplyMode(registry, mode)
	return mode
}
