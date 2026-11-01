//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"strings"
)

const sandboxExecPath = "/usr/bin/sandbox-exec"

type darwinSandbox struct{}

func newPlatformSandbox() Sandbox {
	return &darwinSandbox{}
}

func (s *darwinSandbox) Available() bool {
	_, err := os.Stat(sandboxExecPath)
	return err == nil
}

func buildProfile(config Config) string {
	var sb strings.Builder

	sb.WriteString("(version 1)\n")
	sb.WriteString("(deny default)\n")

	sb.WriteString("(allow process-exec)\n")
	sb.WriteString("(allow process-fork)\n")
	sb.WriteString("(allow sysctl-read)\n")
	sb.WriteString("(allow file-read* (subpath \"/\"))\n")

	for _, path := range config.AllowWrite {
		sb.WriteString(fmt.Sprintf("(allow file-write* (subpath %q))\n", path))
	}

	for _, path := range config.DenyWrite {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			sb.WriteString(fmt.Sprintf("(deny file-write* (subpath %q))\n", path))
		} else {
			sb.WriteString(fmt.Sprintf("(deny file-write* (literal %q))\n", path))
		}
	}

	if config.NetworkEnabled {
		sb.WriteString("(allow network*)\n")
	} else {
		sb.WriteString("(deny network*)\n")
	}

	return sb.String()
}

func (s *darwinSandbox) Wrap(command string, config Config) (string, error) {
	profile := buildProfile(config)
	wrapped := fmt.Sprintf("%s -p '%s' bash -c %q", sandboxExecPath, profile, command)
	return wrapped, nil
}
