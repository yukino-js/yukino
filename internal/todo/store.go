package todo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/uuid"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

const TasksFilename = "tasks.json"

var listIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Store struct {
	path string
}

func NewStore(listID string) (*Store, error) {
	if !listIDPattern.MatchString(listID) {
		return nil, errors.New("Invalid task list ID")
	}
	path, err := storage.SessionPath(listID, TasksFilename)
	if err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

func (s *Store) Load() ([]*Task, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var tasks []*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *Store) Save(tasks []*Task) error {
	if err := storage.MkdirPrivateSync(filepath.Dir(s.path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	tempPath := fmt.Sprintf("%s.%s.tmp", s.path, uuid.NewString())
	if err := storage.WritePrivateFileSync(tempPath, data, os.O_WRONLY|os.O_CREATE|os.O_TRUNC); err != nil {
		os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}
