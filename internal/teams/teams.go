package teams

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/conversation"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/tools"
)

type TeamMode string

const (
	ModeInProcess TeamMode = "in-process"
	ModeTmux      TeamMode = "tmux"
)

const TeamsDirName = "teams"

// TeamsBaseDir is the project-scoped root directory for team data. The
// canonical cwd is hashed so unrelated projects cannot restore, list, mutate,
// or delete each other's teams.
func TeamsBaseDir(cwd string) string {
	key, err := storage.ProjectKey(cwd)
	if err != nil {
		sum := sha256.Sum256([]byte(cwd))
		key = hex.EncodeToString(sum[:])
	}
	return storage.YukinoPath(TeamsDirName, key)
}

type Member struct {
	Name     string
	AgentRef *agent.Agent
	Conv     *conversation.Manager
	Active   bool
	Cancel   context.CancelFunc
	PaneID   string
	Progress *TeammateProgress

	AgentID      string
	AgentType    string
	Model        string
	WorktreePath string
	JoinedAt     int64
}

type Team struct {
	Cwd     string
	Name    string
	Mode    TeamMode
	Members map[string]*Member
	MailBox *FileMailBox
	mu      sync.Mutex

	LeadAgentID string
	Description string
	CreatedAt   int64
}

func NewTeam(cwd, name string, mode TeamMode) *Team {
	inboxDir := filepath.Join(teamDir(cwd, name), "inboxes")
	return &Team{
		Cwd:       cwd,
		Name:      name,
		Mode:      mode,
		Members:   make(map[string]*Member),
		MailBox:   NewFileMailBox(inboxDir),
		CreatedAt: time.Now().Unix(),
	}
}

func (t *Team) AddMember(name string, client llm.Client, registry *tools.Registry, protocol string) *Member {
	t.mu.Lock()
	defer t.mu.Unlock()

	ag := agent.New(client, registry, protocol)
	member := &Member{
		Name:     name,
		AgentRef: ag,
		Conv:     conversation.NewManager(),
		Active:   false,
		AgentID:  name,
		JoinedAt: time.Now().Unix(),
	}
	t.Members[name] = member
	t.persist()
	return member
}

func (t *Team) SetMemberMeta(name, agentType, model, worktreePath string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	member, ok := t.Members[name]
	if !ok {
		return
	}
	member.AgentType = agentType
	member.Model = model
	member.WorktreePath = worktreePath
	t.persist()
}

func (t *Team) StartMember(ctx context.Context, name string, task string) (<-chan agent.AgentEvent, error) {
	t.mu.Lock()
	member, ok := t.Members[name]
	t.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("member not found: %s", name)
	}

	memberCtx, cancel := context.WithCancel(ctx)
	member.Active = true
	member.Cancel = cancel

	t.mu.Lock()
	t.persist()
	t.mu.Unlock()

	member.Conv.AddUserMessage(task)
	ch := member.AgentRef.Run(memberCtx, member.Conv)
	return ch, nil
}

func (t *Team) StopMember(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	member, ok := t.Members[name]
	if !ok {
		return
	}
	if member.PaneID != "" {
		switch t.Mode {
		case ModeTmux:
			stopTmuxTeammate(member.PaneID)
		case ModeITerm:
			stopITermTeammate(member.PaneID)
		}
	}
	if member.Cancel != nil {
		member.Cancel()
	}
	member.Active = false
	t.persist()
}

func (t *Team) GetTeammateProgress() []*TeammateProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	var result []*TeammateProgress
	for _, m := range t.Members {
		if m.Progress != nil {
			result = append(result, m.Progress)
		}
	}
	return result
}

func (t *Team) SendMessage(from, to, content string) {
	t.MailBox.Send(to, FileMailMessage{
		From:      from,
		Text:      content,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

type TeamManager struct {
	mu         sync.Mutex
	cwd        string
	teams      map[string]*Team
	taskStores map[string]*SharedTaskStore
}

func NewTeamManager(cwd string) *TeamManager {
	return &TeamManager{
		cwd:        cwd,
		teams:      make(map[string]*Team),
		taskStores: make(map[string]*SharedTaskStore),
	}
}

func teamDir(cwd, name string) string {
	return filepath.Join(TeamsBaseDir(cwd), sanitizeTeamName(name))
}

func (tm *TeamManager) CreateTeam(name string, mode TeamMode) (*Team, error) {
	return tm.CreateTeamFull(name, mode, "", "")
}

func (tm *TeamManager) CreateTeamFull(name string, mode TeamMode, leadAgentID, description string) (*Team, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	team := NewTeam(tm.cwd, name, mode)
	team.LeadAgentID = leadAgentID
	team.Description = description
	store := NewSharedTaskStore(filepath.Join(teamDir(tm.cwd, name), "tasks.json"))
	if err := store.InitEmpty(); err != nil {
		return nil, err
	}
	tm.teams[name] = team
	tm.taskStores[name] = store
	team.persist()
	return team, nil
}

func (tm *TeamManager) GetTaskStore(teamName string) *SharedTaskStore {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if store, ok := tm.taskStores[teamName]; ok {
		return store
	}
	store := NewSharedTaskStore(filepath.Join(teamDir(tm.cwd, teamName), "tasks.json"))
	tm.taskStores[teamName] = store
	return store
}

func (tm *TeamManager) CreateTeamWith(team *Team) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.teams[team.Name] = team
}

func (tm *TeamManager) GetTeam(name string) *Team {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if team, ok := tm.teams[name]; ok {
		return team
	}
	tf := ReadTeamFile(tm.cwd, name)
	if tf == nil {
		return nil
	}
	team := NewTeam(tm.cwd, tf.Name, ModeInProcess)
	team.LeadAgentID = tf.LeadAgentID
	team.Description = tf.Description
	team.CreatedAt = tf.CreatedAt
	for _, m := range tf.Members {
		if m.BackendType != "" {
			team.Mode = TeamMode(m.BackendType)
		}
		active := false
		if m.IsActive != nil {
			active = *m.IsActive
		}
		team.Members[m.Name] = &Member{
			Name:         m.Name,
			AgentID:      m.AgentID,
			AgentType:    m.AgentType,
			Model:        m.Model,
			WorktreePath: m.WorktreePath,
			JoinedAt:     m.JoinedAt,
			Active:       active,
		}
	}
	tm.teams[name] = team
	return team
}

func (tm *TeamManager) DeleteTeam(name string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if team, ok := tm.teams[name]; ok {
		registry := GetNameRegistry()
		for memberName := range team.Members {
			team.StopMember(memberName)
			registry.Unregister(memberName)
		}
		delete(tm.teams, name)
	}
	delete(tm.taskStores, name)
	_ = os.RemoveAll(teamDir(tm.cwd, name))
}

func (tm *TeamManager) ListTeams() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var names []string
	for name := range tm.teams {
		names = append(names, name)
	}
	return names
}

func (tm *TeamManager) GetAllTeammateProgress() []*TeammateProgress {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var result []*TeammateProgress
	for _, team := range tm.teams {
		result = append(result, team.GetTeammateProgress()...)
	}
	return result
}

func (tm *TeamManager) CloseAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for name, team := range tm.teams {
		for memberName := range team.Members {
			team.StopMember(memberName)
		}
		delete(tm.teams, name)
	}
}
