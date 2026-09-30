//go:build !windows

package common_tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// setWorkflowProcessGroup makes the child the leader of a new process group so
// the whole tree (pnpm -> tsx -> node) can be signalled at once.
func setWorkflowProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killWorkflowProcessGroup sends SIGKILL to every process in the group led by
// pgid. Only the group is signalled (never a bare pid), so a reaped and reused
// pid cannot be hit by mistake.
func killWorkflowProcessGroup(pgid int) error {
	if pgid <= 0 {
		return errors.New("invalid process group id")
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// workflowProcessAlive reports whether a process (or process group) with the
// given id still exists. Uses signal 0, which performs error checking only.
func workflowProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, syscall.Signal(0))
	// EPERM means the process exists but belongs to someone else.
	return err == nil || errors.Is(err, syscall.EPERM)
}

// workflowProcessLooksLikeOurs guards against PID reuse: on Linux it checks
// /proc/<pid>/cmdline for the workflow executor. Where /proc is unavailable it
// falls back to a plain liveness check.
func workflowProcessLooksLikeOurs(pid int) bool {
	if !workflowProcessAlive(pid) {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		if os.IsNotExist(err) {
			if _, statErr := os.Stat("/proc/self"); statErr == nil {
				return false // /proc exists but the pid does not
			}
			return true
		}
		return true
	}
	return strings.Contains(string(data), "workflow_executor")
}
