package lsp

import (
	"fmt"
	"regexp"
	"strings"
)

var FileExtensionPattern = regexp.MustCompile(`^\.[\w-]+$`)

type ServerConfig struct {
	Name                  string
	Command               string
	Args                  []string
	Languages             map[string]string
	Env                   map[string]string
	InitializationOptions map[string]any
	Settings              map[string]any
	TimeoutMs             int
}

func ValidateServerConfig(cfg ServerConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("lsp server name must not be empty")
	}
	if cfg.Command == "" {
		return fmt.Errorf("lsp server '%s': command must not be empty", cfg.Name)
	}
	if len(cfg.Languages) == 0 {
		return fmt.Errorf("lsp server '%s': %s", cfg.Name, FileExtensionRequiredError)
	}
	for ext, language := range cfg.Languages {
		if !FileExtensionPattern.MatchString(ext) {
			return fmt.Errorf("lsp server '%s': invalid file extension '%s'", cfg.Name, ext)
		}
		if language == "" {
			return fmt.Errorf("lsp server '%s': language for '%s' must not be empty", cfg.Name, ext)
		}
	}
	if cfg.TimeoutMs != 0 && (cfg.TimeoutMs < MinTimeoutMs || cfg.TimeoutMs > MaxTimeoutMs) {
		return fmt.Errorf("lsp server '%s': timeout_ms must be between %d and %d", cfg.Name, MinTimeoutMs, MaxTimeoutMs)
	}
	return nil
}
