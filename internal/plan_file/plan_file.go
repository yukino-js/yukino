package plan_file

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

const (
	PlansDirName          = "plans"
	PlanFileExt           = ".md"
	OutsideStorageMessage = "current plan path is outside plan storage"
)

var currentPlanPath string

func plansDir() string {
	return storage.YukinoPath(PlansDirName)
}

func generateSlug() string {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 36)
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return timestamp + "-" + hex.EncodeToString(buf)
}

func isPlanInStorage(planPath string) bool {
	abs, err := filepath.Abs(planPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(plansDir(), abs)
	if err != nil {
		return false
	}
	return rel != "" && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func createPlanPath() string {
	dir := plansDir()
	_ = storage.MkdirPrivateSync(dir)
	path := filepath.Join(dir, generateSlug()+PlanFileExt)
	_ = storage.WritePrivateFileSync(path, nil, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL)
	return path
}

func GetOrCreatePlanPath() string {
	if currentPlanPath != "" {
		if _, err := os.Stat(currentPlanPath); err == nil && isPlanInStorage(currentPlanPath) {
			return currentPlanPath
		}
	}
	currentPlanPath = createPlanPath()
	return currentPlanPath
}

func GetPlanFilePath() string {
	if currentPlanPath != "" {
		return currentPlanPath
	}
	return GetOrCreatePlanPath()
}

func ResetPlanPath() {
	currentPlanPath = ""
}

func PlanExists() bool {
	if currentPlanPath == "" {
		return false
	}
	if _, err := os.Stat(currentPlanPath); err != nil {
		return false
	}
	return isPlanInStorage(currentPlanPath)
}

func LoadPlan() (string, error) {
	if currentPlanPath == "" {
		return "", nil
	}
	if _, err := os.Stat(currentPlanPath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	data, err := storage.ReadPrivateFileSync(currentPlanPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func SavePlan(content string) error {
	path := GetOrCreatePlanPath()
	return storage.WritePrivateFileSync(path, []byte(content), os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

func GetCurrentPlanPath() string {
	return currentPlanPath
}
