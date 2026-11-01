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

type Owner interface {
	CurrentPlanPath() string
	SetPlanFilePath(string)
}

func plansDir() string {
	return storage.YukinoPath(PlansDirName)
}

func generateSlug() string {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 36)
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
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
	return rel != "" && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func CreatePlanPath() (string, error) {
	dir := plansDir()
	if err := storage.MkdirPrivateSync(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, generateSlug()+PlanFileExt)
	if err := storage.WritePrivateFileSync(path, nil, os.O_WRONLY|os.O_CREATE|os.O_EXCL); err != nil {
		return "", err
	}
	return path, nil
}

func GetOrCreatePlanPath(owner Owner) (string, error) {
	currentPlanPath := owner.CurrentPlanPath()
	if currentPlanPath != "" {
		if _, err := os.Stat(currentPlanPath); err == nil && isPlanInStorage(currentPlanPath) {
			return currentPlanPath, nil
		}
	}
	path, err := CreatePlanPath()
	if err != nil {
		return "", err
	}
	owner.SetPlanFilePath(path)
	return path, nil
}

func ResetPlanPath(owner Owner) {
	owner.SetPlanFilePath("")
}

func PlanExists(owner Owner) bool {
	currentPlanPath := owner.CurrentPlanPath()
	if currentPlanPath == "" {
		return false
	}
	if _, err := os.Stat(currentPlanPath); err != nil {
		return false
	}
	return isPlanInStorage(currentPlanPath)
}

func LoadPlan(owner Owner) (string, error) {
	currentPlanPath := owner.CurrentPlanPath()
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

func SavePlan(owner Owner, content string) error {
	path, err := GetOrCreatePlanPath(owner)
	if err != nil {
		return err
	}
	return storage.WritePrivateFileSync(path, []byte(content), os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

func GetCurrentPlanPath(owner Owner) string {
	return owner.CurrentPlanPath()
}
