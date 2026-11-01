package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

const (
	TypeCompactBoundary = "compact_boundary"

	SessionFileExtension = ".jsonl"
	SessionExpiryDays    = 30
)

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type ToolUseRecord struct {
	ToolUseID string         `json:"tool_use_id"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ToolResultRecord struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type Message struct {
	Role        string             `json:"role"`
	Type        string             `json:"type,omitempty"`
	Content     string             `json:"content"`
	Ts          int64              `json:"ts"`
	ToolUses    []ToolUseRecord    `json:"tool_uses,omitempty"`
	ToolResults []ToolResultRecord `json:"tool_results,omitempty"`
}

func FromConversation(msg conversation.Message) Message {
	rec := Message{
		Role:    msg.Role,
		Content: msg.Content,
		Ts:      time.Now().Unix(),
	}
	for _, tu := range msg.ToolUses {
		rec.ToolUses = append(rec.ToolUses, ToolUseRecord{
			ToolUseID: tu.ToolUseID,
			ToolName:  tu.ToolName,
			Arguments: tu.Arguments,
		})
	}
	for _, tr := range msg.ToolResults {
		rec.ToolResults = append(rec.ToolResults, ToolResultRecord{
			ToolUseID: tr.ToolUseID,
			Content:   tr.Content,
			IsError:   tr.IsError,
		})
	}
	return rec
}

func (m Message) ToConversation() conversation.Message {
	msg := conversation.Message{
		Role:    m.Role,
		Content: m.Content,
	}
	for _, tu := range m.ToolUses {
		msg.ToolUses = append(msg.ToolUses, conversation.ToolUseBlock{
			ToolUseID: tu.ToolUseID,
			ToolName:  tu.ToolName,
			Arguments: tu.Arguments,
		})
	}
	for _, tr := range m.ToolResults {
		msg.ToolResults = append(msg.ToolResults, conversation.ToolResultBlock{
			ToolUseID: tr.ToolUseID,
			Content:   tr.Content,
			IsError:   tr.IsError,
		})
	}
	return msg
}

type KeepMessage struct {
	Role        string             `json:"role"`
	Content     string             `json:"content"`
	ToolUses    []ToolUseRecord    `json:"tool_uses,omitempty"`
	ToolResults []ToolResultRecord `json:"tool_results,omitempty"`
}

func FromConversationKeep(msg conversation.Message) KeepMessage {
	rec := FromConversation(msg)
	return KeepMessage{
		Role:        rec.Role,
		Content:     rec.Content,
		ToolUses:    rec.ToolUses,
		ToolResults: rec.ToolResults,
	}
}

type CompactBoundary struct {
	Summary string        `json:"summary"`
	Keep    []KeepMessage `json:"keep"`
}

func SaveCompactBoundary(workDir, sessionID, summary string, keep []KeepMessage) {
	blob, err := json.Marshal(CompactBoundary{Summary: summary, Keep: keep})
	if err != nil {
		return
	}
	SaveMessage(workDir, sessionID, Message{
		Role:    "system",
		Type:    TypeCompactBoundary,
		Content: string(blob),
		Ts:      time.Now().Unix(),
	})
}

func FindLastCompactBoundary(msgs []Message) (boundary CompactBoundary, after []Message, ok bool) {
	last := -1
	for i, m := range msgs {
		if m.Type == TypeCompactBoundary {
			last = i
		}
	}
	if last < 0 {
		return CompactBoundary{}, nil, false
	}
	if err := json.Unmarshal([]byte(msgs[last].Content), &boundary); err != nil {
		return CompactBoundary{}, nil, false
	}
	for _, m := range msgs[last+1:] {
		if m.Type == TypeCompactBoundary {
			continue
		}
		after = append(after, m)
	}
	return boundary, after, true
}

type SessionInfo struct {
	ID           string
	FirstMessage string
	MessageCount int
	FileSize     int64
	GitBranch    string
	ModTime      time.Time
}

func NewID() string {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 36)
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return timestamp + "-" + hex.EncodeToString(buf)
}

func SessionFilePath(workDir, id string) string {
	if !sessionIDRe.MatchString(id) {
		return ""
	}
	dir, err := storage.GetSessionsDir(workDir)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, id+SessionFileExtension)
}

func SaveMessage(workDir, sessionID string, msg Message) {
	path := SessionFilePath(workDir, sessionID)
	if path == "" {
		return
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_ = storage.AppendPrivateFileSync(path, data)
}

func LoadSession(workDir, sessionID string) []Message {
	path := SessionFilePath(workDir, sessionID)
	if path == "" {
		return nil
	}
	data, err := storage.ReadPrivateFileSync(path)
	if err != nil {
		return nil
	}

	var msgs []Message
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		var msg Message
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.Content == "" && len(msg.ToolUses) == 0 && len(msg.ToolResults) == 0 {
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func ListSessions(workDir string) []SessionInfo {
	dir, err := storage.GetSessionsDir(workDir)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	branch := currentGitBranch(workDir)
	cutoff := time.Now().AddDate(0, 0, -SessionExpiryDays)

	var sessions []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		info, err := e.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
			continue
		}

		msgs := LoadSession(workDir, id)
		first := ""
		for _, msg := range msgs {
			if msg.Role == "user" {
				first = msg.Content
				break
			}
		}

		sessions = append(sessions, SessionInfo{
			ID:           id,
			FirstMessage: first,
			MessageCount: len(msgs),
			FileSize:     info.Size(),
			GitBranch:    branch,
			ModTime:      info.ModTime(),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions
}

func currentGitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func FormatRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		weeks := int(d.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	}
}

func FormatFileSize(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		kb := float64(bytes) / 1024
		if kb == float64(int(kb)) {
			return fmt.Sprintf("%.0fKB", kb)
		}
		return fmt.Sprintf("%.1fKB", kb)
	default:
		mb := float64(bytes) / 1024 / 1024
		return fmt.Sprintf("%.1fMB", mb)
	}
}

func MatchesSearch(s SessionInfo, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(s.FirstMessage), q) ||
		strings.Contains(strings.ToLower(s.ID), q)
}
