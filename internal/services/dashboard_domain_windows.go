//go:build windows

package services

import "os/exec"

func configureDashboardCommand(*exec.Cmd) func() { return func() {} }
