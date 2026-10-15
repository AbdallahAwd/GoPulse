//go:build !windows

package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

func executeLocalCron(parent context.Context, job models.CronJob, output *cronOutput) (*int, bool, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(job.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", job.Command)
	cmd.Dir = job.WorkingDir
	if cmd.Dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, false, err
		}
		cmd.Dir = home
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	var code *int
	if cmd.ProcessState != nil {
		value := cmd.ProcessState.ExitCode()
		code = &value
	}
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return code, timedOut, err
}
