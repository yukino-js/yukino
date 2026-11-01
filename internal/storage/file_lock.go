package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	lockAcquireTimeout = 5 * time.Second
	lockStale          = 10 * time.Second
	lockMinBackoff     = 5 * time.Millisecond
	lockMaxBackoff     = 80 * time.Millisecond
)

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

func lockMutexFor(dir string) *sync.Mutex {
	locksMu.Lock()
	defer locksMu.Unlock()
	mu, ok := locks[dir]
	if !ok {
		mu = &sync.Mutex{}
		locks[dir] = mu
	}
	return mu
}

type lockContender struct {
	choosingPath string
	ticketPath   string
	ticketName   string
}

var (
	ticketNumberRe = regexp.MustCompile(`^ticket-(\d+)-`)
	lockOwnerPidRe = regexp.MustCompile(`^(?:choosing-|ticket-\d+-)(\d+)-`)
)

func ticketNumber(entry string) int {
	match := ticketNumberRe.FindStringSubmatch(entry)
	if match == nil {
		return 0
	}
	n, _ := strconv.Atoi(match[1])
	return n
}

func lockOwnerPid(entry string) int {
	match := lockOwnerPidRe.FindStringSubmatch(entry)
	if match == nil {
		return -1
	}
	pid, err := strconv.Atoi(match[1])
	if err != nil || pid <= 0 {
		return -1
	}
	return pid
}

// ownerIdentity is the best-effort process-instance identity recorded in a
// contender file: "<pid>" or "<pid>:<startTick>".
func ownerIdentity() string {
	tick := processStartTick(os.Getpid())
	if tick == "" {
		return strconv.Itoa(os.Getpid())
	}
	return strconv.Itoa(os.Getpid()) + ":" + tick
}

func recordedStartTick(entryPath string) string {
	content, err := os.ReadFile(entryPath)
	if err != nil {
		return ""
	}
	idx := strings.Index(string(content), ":")
	if idx < 0 {
		return ""
	}
	return string(content[idx+1:])
}

func createContender(lockDir string) (*lockContender, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%d-%s", os.Getpid(), hex.EncodeToString(buf))
	choosingPath := filepath.Join(lockDir, "choosing-"+id)
	exclusive := os.O_WRONLY | os.O_CREATE | os.O_TRUNC | os.O_EXCL
	for {
		if err := MkdirPrivateSync(lockDir); err != nil {
			return nil, err
		}
		err := WritePrivateFileSync(choosingPath, []byte(ownerIdentity()), exclusive)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	entries, err := os.ReadDir(lockDir)
	if err != nil {
		os.Remove(choosingPath)
		return nil, err
	}
	maxNumber := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "ticket-") {
			if n := ticketNumber(entry.Name()); n > maxNumber {
				maxNumber = n
			}
		}
	}
	ticketName := fmt.Sprintf("ticket-%016d-%s", maxNumber+1, id)
	ticketPath := filepath.Join(lockDir, ticketName)
	if err := WritePrivateFileSync(ticketPath, []byte(ownerIdentity()), exclusive); err != nil {
		os.Remove(choosingPath)
		os.Remove(ticketPath)
		return nil, err
	}
	os.Remove(choosingPath)
	return &lockContender{choosingPath: choosingPath, ticketPath: ticketPath, ticketName: ticketName}, nil
}

func liveLockEntries(lockDir string, contender *lockContender) (choosing []string, tickets []string) {
	now := time.Now()
	entries, err := os.ReadDir(lockDir)
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		entryPath := filepath.Join(lockDir, entry.Name())
		if entryPath == contender.choosingPath || entryPath == contender.ticketPath {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		ownerPid := lockOwnerPid(entry.Name())
		// A recorded start tick that no longer matches the PID's current
		// occupant proves the original owner exited and the PID was recycled —
		// reclaim it even though the liveness probe now succeeds for the
		// unrelated process.
		recorded := recordedStartTick(entryPath)
		current := ""
		if ownerPid > 0 {
			current = processStartTick(ownerPid)
		}
		pidReused := recorded != "" && current != "" && recorded != current
		ownerDead := ownerPid <= 0 || pidReused || !isProcessAlive(ownerPid)
		if now.Sub(info.ModTime()) > lockStale && ownerDead {
			os.Remove(entryPath)
		}
	}
	entries, err = os.ReadDir(lockDir)
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Name(), "choosing-"):
			choosing = append(choosing, entry.Name())
		case strings.HasPrefix(entry.Name(), "ticket-"):
			tickets = append(tickets, entry.Name())
		}
	}
	sort.Strings(tickets)
	return choosing, tickets
}

func releaseTicket(lockDir string, contender *lockContender) {
	if err := os.Remove(contender.ticketPath); err != nil && !os.IsNotExist(err) {
		// Best effort: another contender may already have reclaimed it.
		_ = err
	}
	// Other contenders still holding tickets keep the directory alive.
	_ = os.Remove(lockDir)
}

func acquireLock(filePath string, wait bool) func() {
	lockDir := filePath + ".lock"
	mu := lockMutexFor(lockDir)
	if wait {
		mu.Lock()
	} else if !mu.TryLock() {
		return nil
	}

	contender, err := createContender(lockDir)
	if err != nil {
		mu.Unlock()
		return nil
	}
	deadline := time.Now().Add(lockAcquireTimeout)
	backoff := lockMinBackoff
	for {
		choosing, tickets := liveLockEntries(lockDir, contender)
		if len(choosing) == 0 && len(tickets) > 0 && tickets[0] == contender.ticketName {
			released := false
			return func() {
				if released {
					return
				}
				releaseTicket(lockDir, contender)
				released = true
				mu.Unlock()
			}
		}
		if !wait || !time.Now().Before(deadline) {
			releaseTicket(lockDir, contender)
			mu.Unlock()
			return nil
		}
		jitter, _ := rand.Int(rand.Reader, big.NewInt(int64(backoff)))
		time.Sleep(backoff + time.Duration(jitter.Int64()))
		backoff *= 2
		if backoff > lockMaxBackoff {
			backoff = lockMaxBackoff
		}
	}
}

// TryAcquireFileSyncLock returns a release function, or nil when the lock is
// held elsewhere.
func TryAcquireFileSyncLock(filePath string) func() {
	return acquireLock(filePath, false)
}

func WithFileSyncLock(filePath string, fn func() error) error {
	release := acquireLock(filePath, true)
	if release == nil {
		return fmt.Errorf("file lock %s: acquisition failed", filePath)
	}
	defer release()
	return fn()
}
