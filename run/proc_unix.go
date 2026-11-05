//go:build unix

package run

import (
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel starts cmd in its own process group and, when its
// context is done, SIGKILLs the whole group — so children of `sh -c` die too.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid = the process group led by cmd.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
