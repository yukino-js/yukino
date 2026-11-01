package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

const (
	TypeCompactBoundary = "compact_boundary"
	// GoalRecord marks goal-state lines, which are not conversation messages.
	GoalRecord = "goal_state"

	SessionFileExtension = ".jsonl"
	SessionExpiryDays    = 30

	FirstMessagePreviewChars = 100
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
	ModTime      time.Time
	// LastUserMessageTime is the newest user input timestamp; nil when the
	// session holds no user messages.
	LastUserMessageTime *time.Time
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

// parseTranscriptRecords returns every parsable record, mirroring
// parseTranscriptRecords in the reference.
func parseTranscriptRecords(path string) []Message {
	data, err := storage.ReadPrivateFileSync(path)
	if err != nil {
		return nil
	}
	var msgs []Message
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg Message
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// LoadSessionForResume reads the transcript under the file lock and renews
// its mtime so an actively resumed session is not swept as expired.
func LoadSessionForResume(workDir, sessionID string) []Message {
	path := SessionFilePath(workDir, sessionID)
	if path == "" {
		return nil
	}
	var msgs []Message
	_ = storage.WithFileSyncLock(path, func() error {
		if _, err := os.Stat(path); err != nil {
			return nil
		}
		for _, msg := range parseTranscriptRecords(path) {
			if msg.Content == "" && len(msg.ToolUses) == 0 && len(msg.ToolResults) == 0 {
				continue
			}
			msgs = append(msgs, msg)
		}
		if len(msgs) > 0 {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
		}
		return nil
	})
	return msgs
}

func LoadSession(workDir, sessionID string) []Message {
	path := SessionFilePath(workDir, sessionID)
	if path == "" {
		return nil
	}
	var msgs []Message
	for _, msg := range parseTranscriptRecords(path) {
		if msg.Content == "" && len(msg.ToolUses) == 0 && len(msg.ToolResults) == 0 {
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// sweptSessionDirs tracks the session directories already swept in this
// process; expired-session cleanup piggybacks on the first ListSessions call
// per directory.
var sweptSessionDirs = struct {
	sync.Mutex
	dirs map[string]bool
}{dirs: make(map[string]bool)}

func ListSessions(workDir string) []SessionInfo {
	dir, err := storage.GetSessionsDir(workDir)
	if err != nil {
		return nil
	}
	sweptSessionDirs.Lock()
	swept := sweptSessionDirs.dirs[dir]
	sweptSessionDirs.dirs[dir] = true
	sweptSessionDirs.Unlock()
	if !swept {
		CleanExpiredSessions(workDir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var sessions []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), SessionFileExtension) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), SessionFileExtension)
		filePath := filepath.Join(dir, e.Name())
		var info *SessionInfo
		_ = storage.WithFileSyncLock(filePath, func() error {
			stat, err := os.Stat(filePath)
			if err != nil {
				return nil
			}
			firstMessage := ""
			messageCount := 0
			lastUserTs := int64(-1)
			for _, msg := range parseTranscriptRecords(filePath) {
				if msg.Type == GoalRecord {
					continue
				}
				messageCount++
				if msg.Role == "user" && len(msg.ToolResults) == 0 && msg.Ts > lastUserTs {
					lastUserTs = msg.Ts
				}
				if firstMessage == "" && msg.Role == "user" && msg.Content != "" {
					firstMessage = string([]rune(msg.Content)[:min(len([]rune(msg.Content)), FirstMessagePreviewChars)])
				}
			}
			entry := SessionInfo{
				ID:           id,
				FirstMessage: firstMessage,
				MessageCount: messageCount,
				FileSize:     stat.Size(),
				ModTime:      stat.ModTime(),
			}
			if lastUserTs >= 0 {
				t := time.Unix(lastUserTs, 0)
				entry.LastUserMessageTime = &t
			}
			info = &entry
			return nil
		})
		if info != nil {
			sessions = append(sessions, *info)
		}
	}

	sort.SliceStable(sessions, func(i, j int) bool {
		ti, tj := int64(-1), int64(-1)
		if sessions[i].LastUserMessageTime != nil {
			ti = sessions[i].LastUserMessageTime.UnixMilli()
		}
		if sessions[j].LastUserMessageTime != nil {
			tj = sessions[j].LastUserMessageTime.UnixMilli()
		}
		if ti != tj {
			return ti > tj
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions
}

// CleanExpiredSessions deletes .jsonl files whose last modified time exceeds
// SessionExpiryDays. Invoked lazily by ListSessions (once per process per
// sessions directory). Failures are skipped (best-effort).
func CleanExpiredSessions(workDir string) int {
	dir, err := storage.GetSessionsDir(workDir)
	if err != nil {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	now := time.Now()
	expiry := time.Duration(SessionExpiryDays) * 24 * time.Hour
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), SessionFileExtension) {
			continue
		}
		filePath := filepath.Join(dir, e.Name())
		_ = storage.WithFileSyncLock(filePath, func() error {
			stat, err := os.Stat(filePath)
			if err != nil {
				return nil
			}
			if now.Sub(stat.ModTime()) <= expiry {
				return nil
			}
			if os.Remove(filePath) == nil {
				removed++
			}
			return nil
		})
	}
	return removed
}
