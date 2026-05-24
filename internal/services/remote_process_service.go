package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/remote"
)

// RemoteProcessService handles process management on a remote server via SSH
type RemoteProcessService struct {
	client *remote.SSHClient
}

// NewRemoteProcessService creates a new RemoteProcessService
func NewRemoteProcessService(client *remote.SSHClient) *RemoteProcessService {
	return &RemoteProcessService{client: client}
}

// ListProcesses returns all running processes from the remote server
func (s *RemoteProcessService) ListProcesses(ctx context.Context) ([]models.ProcessInfo, error) {
	// ps -eo pid,pcpu,pmem,user,etime,comm,args
	// We need to parse this.
	// Output format: PID %CPU %MEM USER ELAPSED COMMAND ARGS
	cmd := "ps -eo pid,pcpu,rss,user,etime,comm,args --no-headers"
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	var result = []models.ProcessInfo{}
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Add debug log if output is empty
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		fmt.Printf("Debug: Process list output is empty\n")
	}

	for i, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			// Log skipped lines for debugging
			fmt.Printf("Debug: Skipping invalid process line %d: %s\n", i, line)
			continue
		}

		pid, _ := strconv.ParseInt(fields[0], 10, 32)
		cpu, _ := strconv.ParseFloat(fields[1], 64)
		rss, _ := strconv.ParseFloat(fields[2], 64) // RSS is in KB
		user := fields[3]
		// elapsed := fields[4] // handling time format is tricky, skipping for now
		// comm := fields[5]

		// Reconstruct command line (args starts from index 6)
		args := strings.Join(fields[6:], " ")
		name := fields[5]

		result = append(result, models.ProcessInfo{
			PID:         int32(pid),
			Name:        name,
			Status:      "running", // ps usually lists running processes
			CPUPercent:  cpu,
			MemoryMB:    rss / 1024,
			Username:    user,
			CommandLine: args,
		})
	}

	return result, nil
}

// GetProcess returns information about a specific process
func (s *RemoteProcessService) GetProcess(ctx context.Context, pid int32) (*models.ProcessInfo, error) {
	// Re-use ListProcesses for simplicity or run specific ps command
	// ps -p PID -o pid,pcpu,rss,user,etime,comm,args --no-headers
	cmd := fmt.Sprintf("ps -p %d -o pid,pcpu,rss,user,etime,comm,args --no-headers", pid)
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("process not found or error: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return nil, fmt.Errorf("process not found")
	}

	fields := strings.Fields(output)
	if len(fields) < 7 {
		return nil, fmt.Errorf("invalid ps output")
	}

	pPid, _ := strconv.ParseInt(fields[0], 10, 32)
	cpu, _ := strconv.ParseFloat(fields[1], 64)
	rss, _ := strconv.ParseFloat(fields[2], 64)
	user := fields[3]
	args := strings.Join(fields[6:], " ")
	name := fields[5]

	return &models.ProcessInfo{
		PID:         int32(pPid),
		Name:        name,
		Status:      "running",
		CPUPercent:  cpu,
		MemoryMB:    rss / 1024,
		Username:    user,
		CommandLine: args,
	}, nil
}

// KillProcess terminates a process by PID
func (s *RemoteProcessService) KillProcess(ctx context.Context, pid int32) error {
	_, err := s.client.RunCommand(fmt.Sprintf("kill %d", pid))
	return err
}

// RestartService restarts a systemd service
func (s *RemoteProcessService) RestartService(ctx context.Context, serviceName string) error {
	// Requires sudo usually, which might fail if user is not root and password input is needed.
	// Assumption: user has sudo without password or is root.

	// Try without sudo first, then with sudo if fails? Or just try directly.
	// Ideally we config if user is root.
	cmd := fmt.Sprintf("sudo systemctl restart %s", serviceName)
	if s.client.IsRoot() { // We don't have IsRoot in SSHClient yet, maybe check user
		cmd = fmt.Sprintf("systemctl restart %s", serviceName)
	}

	_, err := s.client.RunCommand(cmd)
	return err
}

// GetServiceStatus gets the status of a systemd service
func (s *RemoteProcessService) GetServiceStatus(ctx context.Context, serviceName string) (string, error) {
	cmd := fmt.Sprintf("systemctl is-active %s", serviceName)
	output, err := s.client.RunCommand(cmd)
	status := strings.TrimSpace(output)

	if err != nil {
		if status == "" {
			return "unknown", err
		}
		// status might be "inactive" or "failed" with exit code non-zero
		return status, nil
	}

	return status, nil
}
