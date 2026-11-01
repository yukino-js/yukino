//go:build linux

package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
)

type linuxSandbox struct{}

func newPlatformSandbox() Sandbox {
	return &linuxSandbox{}
}

func (s *linuxSandbox) Available() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

func (s *linuxSandbox) Wrap(command string, config Config) (string, error) {
	var args []string

	args = append(args, "bwrap", "--unshare-user", "--unshare-pid")

	args = append(args, "--ro-bind", "/", "/")

	for _, path := range config.AllowWrite {
		args = append(args, "--bind", path, path)
	}

	for _, path := range config.DenyWrite {
		args = append(args, "--ro-bind", path, path)
	}

	if !config.NetworkEnabled {
		args = append(args, "--unshare-net")
	}

	args = append(args, "--proc", "/proc")

	args = append(args, "--", "bash", "-c", command)

	var sb strings.Builder
	for i, arg := range args {
		if i > 0 {
			sb.WriteByte(' ')
		}
		if strings.ContainsAny(arg, " \t\n\"'\\$`!") {
			sb.WriteString(fmt.Sprintf("'%s'", strings.ReplaceAll(arg, "'", "'\\''")))
		} else {
			sb.WriteString(arg)
		}
	}
	return sb.String(), nil
}
