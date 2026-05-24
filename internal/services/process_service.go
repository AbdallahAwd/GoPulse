package services

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/amaa/gopulse/internal/models"
	"github.com/shirou/gopsutil/v3/process"
)

// ProcessService handles process management operations
type ProcessService struct{}

// NewProcessService creates a new ProcessService
func NewProcessService() *ProcessService {
	return &ProcessService{}
}

// ListProcesses returns all running processes
func (s *ProcessService) ListProcesses(ctx context.Context) ([]models.ProcessInfo, error) {
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("failed to get processes: %w", err)
	}

	var result []models.ProcessInfo
	for _, p := range processes {
		info, err := s.getProcessInfo(p)
		if err != nil {
			// Skip processes we can't access
			continue
		}
		result = append(result, info)
	}

	return result, nil
}

// GetProcess returns information about a specific process
func (s *ProcessService) GetProcess(ctx context.Context, pid int32) (*models.ProcessInfo, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("process not found: %w", err)
	}

	info, err := s.getProcessInfo(p)
	if err != nil {
		return nil, fmt.Errorf("failed to get process info: %w", err)
	}

	return &info, nil
}

// KillProcess terminates a process by PID
func (s *ProcessService) KillProcess(ctx context.Context, pid int32) error {
	p, err := process.NewProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}

	if err := p.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	return nil
}

// RestartService restarts a systemd service
func (s *ProcessService) RestartService(ctx context.Context, serviceName string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("service management only supported on Linux")
	}

	cmd := exec.CommandContext(ctx, "systemctl", "restart", serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart service %s: %w, output: %s", serviceName, err, string(output))
	}

	return nil
}

// GetServiceStatus gets the status of a systemd service
func (s *ProcessService) GetServiceStatus(ctx context.Context, serviceName string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("service management only supported on Linux")
	}

	cmd := exec.CommandContext(ctx, "systemctl", "is-active", serviceName)
	output, err := cmd.Output()
	status := strings.TrimSpace(string(output))

	if err != nil {
		// systemctl returns non-zero for inactive services
		if status == "" {
			return "unknown", err
		}
		return status, nil
	}

	return status, nil
}

// getProcessInfo extracts information from a process handle
func (s *ProcessService) getProcessInfo(p *process.Process) (models.ProcessInfo, error) {
	info := models.ProcessInfo{
		PID: p.Pid,
	}

	// Get process name
	if name, err := p.Name(); err == nil {
		info.Name = name
	}

	// Get process status
	if status, err := p.Status(); err == nil {
		if len(status) > 0 {
			info.Status = status[0]
		}
	}

	// Get CPU percent
	if cpuPercent, err := p.CPUPercent(); err == nil {
		info.CPUPercent = cpuPercent
	}

	// Get memory info
	if memInfo, err := p.MemoryInfo(); err == nil {
		info.MemoryMB = float64(memInfo.RSS) / 1024 / 1024
	}

	// Get username
	if username, err := p.Username(); err == nil {
		info.Username = username
	}

	// Get create time
	if createTime, err := p.CreateTime(); err == nil {
		info.CreateTime = createTime
	}

	// Get command line
	if cmdline, err := p.Cmdline(); err == nil {
		info.CommandLine = cmdline
	}

	return info, nil
}

// SearchProcesses searches for processes by name
func (s *ProcessService) SearchProcesses(ctx context.Context, query string) ([]models.ProcessInfo, error) {
	allProcesses, err := s.ListProcesses(ctx)
	if err != nil {
		return nil, err
	}

	var result []models.ProcessInfo
	query = strings.ToLower(query)

	for _, p := range allProcesses {
		if strings.Contains(strings.ToLower(p.Name), query) ||
			strings.Contains(strings.ToLower(p.CommandLine), query) {
			result = append(result, p)
		}
	}

	return result, nil
}
