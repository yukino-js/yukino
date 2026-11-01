package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/yukino-js/yukino/yukino/internal/storage"
	"github.com/yukino-js/yukino/yukino/internal/utils"
)

const (
	MaxHistoryEntries     = 10_000
	CompactAfterRecords   = 20_000
	ActivityFileName      = "activity.jsonl"
	InvalidProjectMessage = "Activity project key does not match its working directory."
)

var (
	projectKeyRe = regexp.MustCompile(`^[a-f0-9]{64}$`)
	sessionIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

var validSources = map[string]bool{"cli": true, "ws": true, "print": true, "sdk": true}

type ProjectInfo struct {
	ProjectKey string `json:"projectKey"`
	Cwd        string `json:"cwd"`
	CreatedAt  int64  `json:"createdAt"`
}

type InputEntry struct {
	Type        string  `json:"type"`
	ID          string  `json:"id"`
	Timestamp   int64   `json:"timestamp"`
	Cwd         string  `json:"cwd"`
	SessionID   *string `json:"sessionId"`
	Source      string  `json:"source"`
	Text        string  `json:"text"`
	CommandName *string `json:"commandName"`
}

type InputOptions struct {
	Cwd         string
	SessionID   *string
	Source      string
	Text        string
	CommandName *string
}

type CommandUsage struct {
	UsageCount int   `json:"usageCount"`
	LastUsedAt int64 `json:"lastUsedAt"`
}

type ActivityState struct {
	Projects     map[string]ProjectInfo  `json:"projects"`
	Inputs       []InputEntry            `json:"inputs"`
	CommandUsage map[string]CommandUsage `json:"commandUsage"`
}

type projectRecord struct {
	Type       string `json:"type"`
	ProjectKey string `json:"projectKey"`
	Cwd        string `json:"cwd"`
	CreatedAt  int64  `json:"createdAt"`
}

type checkpointRecord struct {
	Type         string                  `json:"type"`
	Projects     map[string]ProjectInfo  `json:"projects"`
	Inputs       []InputEntry            `json:"inputs"`
	CommandUsage map[string]CommandUsage `json:"commandUsage"`
}

type loadedActivity struct {
	signature     string
	state         ActivityState
	recordCount   int
	completeBytes int64
}

var (
	cacheMu        sync.Mutex
	cachedActivity = map[string]*loadedActivity{}
)

func emptyState() ActivityState {
	return ActivityState{
		Projects:     map[string]ProjectInfo{},
		Inputs:       []InputEntry{},
		CommandUsage: map[string]CommandUsage{},
	}
}

func activityFilePath() string {
	return storage.YukinoPath(ActivityFileName)
}

func fileSignature(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return platformFileSignature(path, info)
}

func validateProject(key string, project ProjectInfo) error {
	if !projectKeyRe.MatchString(project.ProjectKey) {
		return errors.New("invalid projectKey")
	}
	if !filepath.IsAbs(project.Cwd) {
		return errors.New("cwd must be absolute")
	}
	if project.CreatedAt < 0 {
		return errors.New("negative createdAt")
	}
	if key != project.ProjectKey {
		return errors.New(InvalidProjectMessage)
	}
	canonical, err := utils.CanonicalPath(project.Cwd, 0)
	if err != nil {
		return err
	}
	if actual, err := storage.ProjectKey(canonical); err != nil || actual != key {
		return errors.New(InvalidProjectMessage)
	}
	return nil
}

func applyRecord(state ActivityState, raw []byte) (ActivityState, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return state, err
	}
	switch head.Type {
	case "checkpoint":
		var record checkpointRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return state, err
		}
		next := emptyState()
		for key, project := range record.Projects {
			if err := validateProject(key, project); err != nil {
				continue
			}
			next.Projects[key] = project
		}
		next.Inputs = record.Inputs
		next.CommandUsage = record.CommandUsage
		if next.CommandUsage == nil {
			next.CommandUsage = map[string]CommandUsage{}
		}
		return next, nil
	case "project":
		var record projectRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return state, err
		}
		project := ProjectInfo{ProjectKey: record.ProjectKey, Cwd: record.Cwd, CreatedAt: record.CreatedAt}
		if err := validateProject(record.ProjectKey, project); err != nil {
			return state, err
		}
		if _, exists := state.Projects[record.ProjectKey]; !exists {
			state.Projects[record.ProjectKey] = project
		}
		return state, nil
	case "input":
		var record InputEntry
		if err := json.Unmarshal(raw, &record); err != nil {
			return state, err
		}
		if err := validateInput(record); err != nil {
			return state, err
		}
		state.Inputs = append(state.Inputs, record)
		if len(state.Inputs) > MaxHistoryEntries {
			state.Inputs = state.Inputs[len(state.Inputs)-MaxHistoryEntries:]
		}
		if record.CommandName != nil {
			entry := state.CommandUsage[*record.CommandName]
			entry.UsageCount++
			if record.Timestamp > entry.LastUsedAt {
				entry.LastUsedAt = record.Timestamp
			}
			state.CommandUsage[*record.CommandName] = entry
		}
		return state, nil
	}
	return state, fmt.Errorf("unknown activity record type %q", head.Type)
}

func validateInput(record InputEntry) error {
	if _, err := uuid.Parse(record.ID); err != nil {
		return errors.New("invalid input id")
	}
	if record.Timestamp < 0 {
		return errors.New("negative timestamp")
	}
	if !filepath.IsAbs(record.Cwd) {
		return errors.New("cwd must be absolute")
	}
	if record.SessionID != nil && !sessionIDRe.MatchString(*record.SessionID) {
		return errors.New("invalid sessionId")
	}
	if !validSources[record.Source] {
		return fmt.Errorf("invalid source %q", record.Source)
	}
	if record.Text == "" {
		return errors.New("text must not be empty")
	}
	if record.CommandName != nil && *record.CommandName == "" {
		return errors.New("commandName must not be empty")
	}
	return nil
}

func readActivity(filePath string) *loadedActivity {
	if _, err := os.Stat(filePath); err != nil {
		return &loadedActivity{state: emptyState()}
	}
	current := fileSignature(filePath)
	cacheMu.Lock()
	cached, ok := cachedActivity[filePath]
	cacheMu.Unlock()
	if ok && cached.signature == current {
		return cached
	}
	content, err := storage.ReadPrivateFileSync(filePath)
	if err != nil {
		return &loadedActivity{state: emptyState()}
	}
	completeBytes := int64(strings.LastIndex(string(content), "\n")) + 1
	state := emptyState()
	recordCount := 0
	for _, line := range strings.Split(string(content[:completeBytes]), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		recordCount++
		next, err := applyRecord(state, []byte(line))
		if err != nil {
			continue
		}
		state = next
	}
	loaded := &loadedActivity{
		signature:     fileSignature(filePath),
		state:         state,
		recordCount:   recordCount,
		completeBytes: completeBytes,
	}
	cacheMu.Lock()
	cachedActivity[filePath] = loaded
	cacheMu.Unlock()
	return loaded
}

func writeCheckpoint(filePath string, loaded *loadedActivity) error {
	tempPath := fmt.Sprintf("%s.%d.%s.tmp", filePath, os.Getpid(), uuid.NewString())
	record := checkpointRecord{
		Type:         "checkpoint",
		Projects:     loaded.state.Projects,
		Inputs:       loaded.state.Inputs,
		CommandUsage: loaded.state.CommandUsage,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	defer os.Remove(tempPath)
	if err := storage.WritePrivateFileSync(tempPath, data, os.O_WRONLY|os.O_CREATE|os.O_TRUNC); err != nil {
		return err
	}
	if err := os.Rename(tempPath, filePath); err != nil {
		return err
	}
	loaded.recordCount = 1
	if info, err := os.Stat(filePath); err == nil {
		loaded.completeBytes = info.Size()
	}
	return nil
}

func updateActivity(buildRecords func(state ActivityState) [][]byte, compact bool) error {
	if err := storage.MkdirPrivateSync(storage.GetYukinoDir()); err != nil {
		return err
	}
	filePath := activityFilePath()
	return storage.WithFileSyncLock(filePath, func() error {
		loaded := readActivity(filePath)
		records := buildRecords(loaded.state)
		if info, err := os.Stat(filePath); err == nil && info.Size() != loaded.completeBytes {
			// Drop a torn trailing line so appends stay parseable.
			if f, err := os.OpenFile(filePath, os.O_WRONLY, 0); err == nil {
				_ = f.Truncate(loaded.completeBytes)
				f.Close()
			}
		}
		if len(records) > 0 {
			var content []byte
			for _, record := range records {
				content = append(content, record...)
				content = append(content, '\n')
			}
			if err := storage.AppendPrivateFileSync(filePath, content); err != nil {
				cacheMu.Lock()
				delete(cachedActivity, filePath)
				cacheMu.Unlock()
				return err
			}
			for _, record := range records {
				if next, err := applyRecord(loaded.state, record); err == nil {
					loaded.state = next
				}
			}
			loaded.recordCount += len(records)
			loaded.completeBytes += int64(len(content))
		}
		if compact || loaded.recordCount >= CompactAfterRecords {
			if err := writeCheckpoint(filePath, loaded); err != nil {
				if len(records) == 0 {
					cacheMu.Lock()
					delete(cachedActivity, filePath)
					cacheMu.Unlock()
					return err
				}
			}
		}
		if _, err := os.Stat(filePath); err == nil {
			loaded.signature = fileSignature(filePath)
			cacheMu.Lock()
			cachedActivity[filePath] = loaded
			cacheMu.Unlock()
		}
		return nil
	})
}

func projectRecords(cwd string, state ActivityState) [][]byte {
	key, err := storage.ProjectKey(cwd)
	if err != nil {
		return nil
	}
	if _, exists := state.Projects[key]; exists {
		return nil
	}
	record := projectRecord{
		Type:       "project",
		Cwd:        cwd,
		ProjectKey: key,
		CreatedAt:  time.Now().UnixMilli(),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil
	}
	return [][]byte{data}
}

func RegisterProject(cwd string) error {
	canonical, err := utils.CanonicalPath(cwd, 0)
	if err != nil {
		return err
	}
	return updateActivity(func(state ActivityState) [][]byte {
		return projectRecords(canonical, state)
	}, false)
}

func RecordInput(options InputOptions) (InputEntry, error) {
	canonical, err := utils.CanonicalPath(options.Cwd, 0)
	if err != nil {
		return InputEntry{}, err
	}
	entry := InputEntry{
		Type:        "input",
		ID:          uuid.NewString(),
		Timestamp:   time.Now().UnixMilli(),
		Cwd:         canonical,
		SessionID:   options.SessionID,
		Source:      options.Source,
		Text:        options.Text,
		CommandName: options.CommandName,
	}
	if err := validateInput(entry); err != nil {
		return InputEntry{}, err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return InputEntry{}, err
	}
	err = updateActivity(func(state ActivityState) [][]byte {
		return append(projectRecords(canonical, state), data)
	}, false)
	if err != nil {
		return InputEntry{}, err
	}
	return entry, nil
}

func queryState(read func(state ActivityState)) {
	filePath := activityFilePath()
	if _, err := os.Stat(filePath); err != nil {
		read(emptyState())
		return
	}
	_ = storage.WithFileSyncLock(filePath, func() error {
		read(readActivity(filePath).state)
		return nil
	})
}

func LoadInputs() []InputEntry {
	var inputs []InputEntry
	queryState(func(state ActivityState) {
		inputs = append([]InputEntry{}, state.Inputs...)
	})
	return inputs
}

func LoadInputTexts() []string {
	var texts []string
	for _, input := range LoadInputs() {
		if len(texts) == 0 || texts[len(texts)-1] != input.Text {
			texts = append(texts, input.Text)
		}
	}
	return texts
}

func GetCommandUsage() map[string]CommandUsage {
	usage := map[string]CommandUsage{}
	queryState(func(state ActivityState) {
		for name, entry := range state.CommandUsage {
			usage[name] = entry
		}
	})
	return usage
}

func ListProjects() []ProjectInfo {
	var projects []ProjectInfo
	queryState(func(state ActivityState) {
		for _, project := range state.Projects {
			projects = append(projects, project)
		}
	})
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].ProjectKey < projects[j].ProjectKey
	})
	return projects
}

func CompactActivity() error {
	return updateActivity(func(state ActivityState) [][]byte {
		return nil
	}, true)
}
