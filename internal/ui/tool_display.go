package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type subAgentBlock struct {
	desc      string
	agentType string
	toolUses  []toolBlockInfo
	done      bool
	toolCount int
	totalTime float64
}

type toolBlockInfo struct {
	toolName  string
	args      map[string]any
	output    string
	isError   bool
	elapsed   float64
	collapsed bool
	loading   bool
}

func renderToolBlockText(tb toolBlockInfo) string {
	title := toolTitle(tb.toolName, tb.args)
	if tb.isError {
		return fmt.Sprintf("✗ %s (%.1fs)", title, tb.elapsed)
	}
	return fmt.Sprintf("✓ %s (%.1fs)", title, tb.elapsed)
}

func renderSubAgentBlock(sab *subAgentBlock, expanded bool) string {
	var sb strings.Builder

	agentLabel := strings.Title(sab.agentType)
	if agentLabel == "" {
		agentLabel = "Agent"
	}
	header := lipgloss.NewStyle().Foreground(brandPurple).Bold(true).Render(
		fmt.Sprintf("● %s(%s)", agentLabel, sab.desc))
	sb.WriteString(header)
	sb.WriteString("\n")

	if sab.done {
		if expanded {
			for _, tu := range sab.toolUses {
				title := toolTitle(tu.toolName, tu.args)
				line := fmt.Sprintf("     %s (%.1fs)", title, tu.elapsed)
				if tu.isError {
					sb.WriteString(toolErrorStyle.Render(line))
				} else {
					sb.WriteString(toolDoneStyle.Render(line))
				}
				sb.WriteString("\n")
			}
		} else {
			summary := fmt.Sprintf("  ⎿  Done (%d tool uses · %.1fs)", sab.toolCount, sab.totalTime)
			sb.WriteString(toolDoneStyle.Render(summary))
			sb.WriteString(lipgloss.NewStyle().Foreground(dimText).Render("  (ctrl+o to expand)"))
			sb.WriteString("\n")
		}
	} else {
		n := len(sab.toolUses)
		if n > 0 {
			last := sab.toolUses[n-1]
			lastTitle := toolTitle(last.toolName, last.args)
			sb.WriteString(toolDoneStyle.Render(fmt.Sprintf("  ⎿  %s (%.1fs)", lastTitle, last.elapsed)))
			sb.WriteString("\n")
		}
		if n > 1 {
			sb.WriteString(lipgloss.NewStyle().Foreground(dimText).Render(
				fmt.Sprintf("     … +%d tool uses (ctrl+o to expand)", n-1)))
			sb.WriteString("\n")
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(dimText).Render("     Running…"))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func isCollapsibleTool(name string) bool {
	switch name {
	case "ReadFile", "Glob", "Grep", "ToolSearch":
		return true
	}
	return false
}

func isDiffTool(name string) bool {
	return name == "EditFile"
}

func renderDiffLines(output string) string {
	lines := strings.Split(output, "\n")
	rendered := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+ "):
			rendered[i] = diffAddStyle.Render(line)
		case strings.HasPrefix(line, "- "):
			rendered[i] = diffRemoveStyle.Render(line)
		default:
			rendered[i] = toolDetailStyle.Render(line)
		}
	}
	return strings.Join(rendered, "\n")
}

func appendEditDiff(sb *strings.Builder, toolGroup []toolBlockInfo) {
	if len(toolGroup) != 1 {
		return
	}
	tb := toolGroup[0]
	if !isDiffTool(tb.toolName) || tb.output == "" {
		return
	}
	sb.WriteString(renderDiffLines(tb.output))
	sb.WriteString("\n")
}

func renderToolGroupSummary(tools []toolBlockInfo) string {
	var totalElapsed float64
	errors := 0
	for _, tb := range tools {
		totalElapsed += tb.elapsed
		if tb.isError {
			errors++
		}
	}
	n := len(tools)
	if errors > 0 {
		return fmt.Sprintf("● Done (%d tool uses · %d errors · %.1fs)", n, errors, totalElapsed)
	}
	return fmt.Sprintf("● Done (%d tool uses · %.1fs)", n, totalElapsed)
}

func (m Model) renderToolBlock(tb toolBlockInfo) string {
	title := toolTitle(tb.toolName, tb.args)

	if tb.loading {
		return toolRunningStyle.Render(fmt.Sprintf("● %s …", title))
	}

	if tb.isError {
		return toolErrorStyle.Render(fmt.Sprintf("✗ %s — error (%.1fs)", title, tb.elapsed))
	}

	return toolDoneStyle.Render(fmt.Sprintf("✓ %s (%.1fs)", title, tb.elapsed))
}

func toolTitle(toolName string, args map[string]any) string {
	switch toolName {
	case "ReadFile":
		p, _ := args["file_path"].(string)
		if p != "" {
			return "Read " + filepath.Base(p)
		}
		return "Read"
	case "WriteFile":
		p, _ := args["file_path"].(string)
		content, _ := args["content"].(string)
		lines := strings.Count(content, "\n") + 1
		if p != "" {
			return fmt.Sprintf("Write %s (%d lines)", filepath.Base(p), lines)
		}
		return "Write"
	case "EditFile":
		p, _ := args["file_path"].(string)
		if p != "" {
			return "Edit " + filepath.Base(p)
		}
		return "Edit"
	case "Bash":
		cmd, _ := args["command"].(string)
		if len(cmd) > 50 {
			cmd = cmd[:50] + "…"
		}
		if cmd != "" {
			return fmt.Sprintf("Bash: %s", cmd)
		}
		return "Bash"
	case "Glob":
		pattern, _ := args["pattern"].(string)
		return fmt.Sprintf("Glob: %s", pattern)
	case "Grep":
		pattern, _ := args["pattern"].(string)
		return fmt.Sprintf("Grep: %s", pattern)
	}
	return toolName
}

func indentBlock(text string, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
