//go:build !unix

package lsp

import (
	"os/exec"
	"strconv"
)

func prepareProcess(cmd *exec.Cmd) {}
func killProcess(cmd *exec.Cmd) {
	if exec.Command("taskkill", "/pid", strconv.Itoa(cmd.Process.Pid), "/t", "/f").Run() != nil {
		_ = cmd.Process.Kill()
	}
}
