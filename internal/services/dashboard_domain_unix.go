//go:build !windows

package services

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Give the shell's rollback trap a chance to run on cancellation. Bound the
// entire process group so package/certificate children cannot run indefinitely.
func configureDashboardCommand(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var mu sync.Mutex
	var timer *time.Timer
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		mu.Lock()
		timer = time.AfterFunc(5*time.Second, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		mu.Unlock()
		return err
	}
	cmd.WaitDelay = 8 * time.Second
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	}
}
