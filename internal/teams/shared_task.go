package teams

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type SharedTask struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Assignee    string   `json:"assignee"`
	Blocks      []string `json:"blocks"`
	BlockedBy   []string `json:"blocked_by"`
	CreatedBy   string   `json:"created_by"`
}

type storeData struct {
	NextID int          `json:"next_id"`
	Tasks  []SharedTask `json:"tasks"`
}

type SharedTaskStore struct {
	mu     sync.Mutex
	path   string
	nextID int
	tasks  []SharedTask
}

func NewSharedTaskStore(path string) *SharedTaskStore {
	s := &SharedTaskStore{path: path, nextID: 1}
	s.load()
	return s
}

func (s *SharedTaskStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var sd storeData
	if err := json.Unmarshal(data, &sd); err != nil {
		return
	}
	s.tasks = sd.Tasks
	if sd.NextID > 0 {
		s.nextID = sd.NextID
	}
}

func (s *SharedTaskStore) save() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	sd := storeData{NextID: s.nextID, Tasks: s.tasks}
	if s.tasks == nil {
		sd.Tasks = []SharedTask{}
	}
	out, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, out, 0o644)
}

func (s *SharedTaskStore) Create(title, description, assignee string, blocks, blockedBy []string, createdBy string) SharedTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	if blocks == nil {
		blocks = []string{}
	}
	if blockedBy == nil {
		blockedBy = []string{}
	}
	task := SharedTask{
		ID:          strconv.Itoa(s.nextID),
		Title:       title,
		Description: description,
		Status:      "pending",
		Assignee:    assignee,
		Blocks:      blocks,
		BlockedBy:   blockedBy,
		CreatedBy:   createdBy,
	}
	s.nextID++
	s.tasks = append(s.tasks, task)
	s.save()
	return task
}

func (s *SharedTaskStore) Get(id string) *SharedTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			t := s.tasks[i]
			return &t
		}
	}
	return nil
}

func (s *SharedTaskStore) ListTasks(status, assignee string) []SharedTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	var result []SharedTask
	for _, t := range s.tasks {
		if status != "" && t.Status != status {
			continue
		}
		if assignee != "" && t.Assignee != assignee {
			continue
		}
		result = append(result, t)
	}
	return result
}

type TaskUpdate struct {
	Status       *string
	Assignee     *string
	Description  *string
	AddBlocks    []string
	AddBlockedBy []string
}

func (s *SharedTaskStore) Update(id string, upd TaskUpdate) *SharedTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	for i := range s.tasks {
		if s.tasks[i].ID != id {
			continue
		}
		t := &s.tasks[i]
		if upd.Status != nil {
			t.Status = *upd.Status
		}
		if upd.Assignee != nil {
			t.Assignee = *upd.Assignee
		}
		if upd.Description != nil {
			t.Description = *upd.Description
		}
		t.Blocks = appendUnique(t.Blocks, upd.AddBlocks)
		t.BlockedBy = appendUnique(t.BlockedBy, upd.AddBlockedBy)
		s.save()
		updated := *t
		return &updated
	}
	return nil
}

func (s *SharedTaskStore) InitEmpty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = []SharedTask{}
	s.nextID = 1
	s.save()
}

func appendUnique(base, add []string) []string {
	for _, v := range add {
		found := false
		for _, e := range base {
			if e == v {
				found = true
				break
			}
		}
		if !found {
			base = append(base, v)
		}
	}
	if base == nil {
		base = []string{}
	}
	return base
}
