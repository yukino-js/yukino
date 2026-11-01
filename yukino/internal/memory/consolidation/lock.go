package consolidation

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const lockFileName = ".consolidate-lock"

const holderStaleMs = 60 * 60 * 1000

func lockPath(memoryDir string) string {
	return filepath.Join(memoryDir, lockFileName)
}

func ReadLastConsolidatedAt(memoryDir string) (int64, error) {
	info, err := os.Stat(lockPath(memoryDir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.ModTime().UnixMilli(), nil
}

func TryAcquireLock(memoryDir string) (priorMtime int64, err error) {
	path := lockPath(memoryDir)

	var mtimeMs int64
	var holderPid int
	var fileExists bool

	if info, statErr := os.Stat(path); statErr == nil {
		fileExists = true
		mtimeMs = info.ModTime().UnixMilli()
		if raw, readErr := os.ReadFile(path); readErr == nil {
			if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw))); parseErr == nil {
				holderPid = pid
			}
		}
	}

	if fileExists && time.Now().UnixMilli()-mtimeMs < holderStaleMs {
		if holderPid > 0 && isProcessRunning(holderPid) {
			return -1, nil
		}
	}

	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return -1, fmt.Errorf("write lock: %w", err)
	}

	verify, err := os.ReadFile(path)
	if err != nil {
		return -1, fmt.Errorf("verify lock: %w", err)
	}
	if strings.TrimSpace(string(verify)) != strconv.Itoa(os.Getpid()) {
		return -1, nil
	}

	if fileExists {
		return mtimeMs, nil
	}
	return 0, nil
}

func RollbackLock(memoryDir string, priorMtime int64) {
	path := lockPath(memoryDir)
	if priorMtime == 0 {
		os.Remove(path)
		return
	}
	os.WriteFile(path, []byte(""), 0o644)
	t := time.UnixMilli(priorMtime)
	os.Chtimes(path, t, t)
}
