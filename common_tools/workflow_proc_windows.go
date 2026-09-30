//go:build windows

package common_tools

import (
	"errors"
	"os"
	"os/exec"
)

func setWorkflowProcessGroup(cmd *exec.Cmd) {}

func killWorkflowProcessGroup(pgid int) error {
	if pgid <= 0 {
		return errors.New("invalid process id")
	}
	proc, err := os.FindProcess(pgid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// workflowProcessAlive is best-effort on Windows: FindProcess opens a handle
// and fails if the process no longer exists.
func workflowProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = proc.Release()
	return true
}

func workflowProcessLooksLikeOurs(pid int) bool { return workflowProcessAlive(pid) }
