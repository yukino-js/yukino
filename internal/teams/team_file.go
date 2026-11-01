package teams

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/permissions"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

type TeamFile struct {
	Name           string                     `json:"name"`
	Description    string                     `json:"description,omitempty"`
	CreatedAt      int64                      `json:"createdAt"`
	LeadAgentID    string                     `json:"leaderAgentId"`
	PermissionMode permissions.PermissionMode `json:"permissionMode"`
	Members        []TeamMemberFile           `json:"members"`
}

type TeamMemberFile struct {
	AgentID      string `json:"agentId"`
	Name         string `json:"name"`
	AgentType    string `json:"agentType,omitempty"`
	Model        string `json:"model,omitempty"`
	JoinedAt     int64  `json:"joinedAt"`
	WorktreePath string `json:"worktreePath,omitempty"`
	IsActive     *bool  `json:"isActive,omitempty"`
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

func sanitizeTeamName(name string) string {
	return strings.ToLower(nonAlnum.ReplaceAllString(name, "-"))
}

const TeamConfigFilename = "config.json"

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%0*x", n*2, 0)
	}
	return hex.EncodeToString(buf)
}

func teamConfigPath(cwd, name string) string {
	return filepath.Join(teamDir(cwd, name), TeamConfigFilename)
}

// ReadTeamFile reads team configuration. It returns nil when the file does
// not exist or fails to parse, allowing the caller to treat it as "team not
// found" rather than propagating an exception.
func ReadTeamFile(cwd, name string) *TeamFile {
	return ReadTeamFileAtPath(teamConfigPath(cwd, name))
}

func ReadTeamFileAtPath(path string) *TeamFile {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	data, err := storage.ReadPrivateFileSync(path)
	if err != nil {
		return nil
	}
	var tf TeamFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil
	}
	return &tf
}

// WriteTeamFile writes team configuration under the same cross-process lock
// used by other shared team files. The temporary file is in the destination
// directory, so rename publishes a complete JSON document atomically to
// unlocked readers. Persistence remains best-effort for the in-memory team.
func WriteTeamFile(cwd, name string, file *TeamFile, removedMembers ...string) {
	path := teamConfigPath(cwd, name)
	if err := storage.MkdirPrivateSync(filepath.Dir(path)); err != nil {
		return
	}
	_ = storage.WithFileSyncLock(path, func() error {
		next := file
		if current := ReadTeamFileAtPath(path); current != nil {
			incoming := map[string]bool{}
			for _, member := range file.Members {
				incoming[member.Name] = true
			}
			removed := map[string]bool{}
			for _, member := range removedMembers {
				removed[member] = true
			}
			merged := make([]TeamMemberFile, 0, len(current.Members)+len(file.Members))
			for _, member := range current.Members {
				if !incoming[member.Name] && !removed[member.Name] {
					merged = append(merged, member)
				}
			}
			merged = append(merged, file.Members...)
			nextFile := *file
			nextFile.Members = merged
			next = &nextFile
		}
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		tmpPath := fmt.Sprintf("%s.%d.%s.tmp", path, os.Getpid(), randomHex(8))
		defer os.Remove(tmpPath)
		if err := storage.WritePrivateFileSync(tmpPath, data, os.O_WRONLY|os.O_CREATE|os.O_TRUNC); err != nil {
			return err
		}
		return os.Rename(tmpPath, path)
	})
}

func (t *Team) snapshot() *TeamFile {
	tf := &TeamFile{
		Name:           t.Name,
		Description:    t.Description,
		CreatedAt:      t.CreatedAt,
		LeadAgentID:    t.LeadAgentID,
		PermissionMode: t.PermissionMode,
		Members:        make([]TeamMemberFile, 0, len(t.Members)),
	}
	if tf.CreatedAt == 0 {
		tf.CreatedAt = time.Now().Unix()
	}
	for _, m := range t.Members {
		active := m.Active
		tf.Members = append(tf.Members, TeamMemberFile{
			AgentID:      m.AgentID,
			Name:         m.Name,
			AgentType:    m.AgentType,
			Model:        m.Model,
			JoinedAt:     m.JoinedAt,
			WorktreePath: m.WorktreePath,
			IsActive:     &active,
		})
	}
	return tf
}

func (t *Team) persist() {
	WriteTeamFile(t.Cwd, t.Name, t.snapshot())
}
