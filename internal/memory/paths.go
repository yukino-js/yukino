package memory

import (
	"path/filepath"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

const (
	AutoMemEntrypointName = "MEMORY.md"
	MemoryDirName         = "memory"
)

func GetAutoMemPath(projectRoot string) string {
	dir, err := storage.ProjectPath(projectRoot, MemoryDirName)
	if err != nil {
		return ""
	}
	return dir + string(filepath.Separator)
}

func GetAutoMemEntrypoint(projectRoot string) string {
	dir := GetAutoMemPath(projectRoot)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, AutoMemEntrypointName)
}

func IsAutoMemPath(absolutePath, projectRoot string) bool {
	abs := filepath.Clean(absolutePath)
	if dir := GetAutoMemPath(projectRoot); dir != "" {
		if strings.HasPrefix(abs+string(filepath.Separator), dir) {
			return true
		}
	}
	if dir := GetUserAutoMemPath(); dir != "" {
		if strings.HasPrefix(abs+string(filepath.Separator), dir) {
			return true
		}
	}
	return false
}

func GetUserAutoMemPath() string {
	return storage.YukinoPath(MemoryDirName) + string(filepath.Separator)
}

func GetUserAutoMemEntrypoint() string {
	dir := GetUserAutoMemPath()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, AutoMemEntrypointName)
}

func IsUserAutoMemPath(absolutePath string) bool {
	dir := GetUserAutoMemPath()
	if dir == "" {
		return false
	}
	abs := filepath.Clean(absolutePath)
	return strings.HasPrefix(abs+string(filepath.Separator), dir)
}
