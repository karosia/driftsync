//go:build !unix

package run

import "os/exec"

// killProcessGroupOnCancel: no process groups here; exec's default Cancel
// kills the direct child, and WaitDelay bounds any straggler holding a pipe.
func killProcessGroupOnCancel(cmd *exec.Cmd) {}
