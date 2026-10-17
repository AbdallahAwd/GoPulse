package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
)

// RemoteStatsService handles system statistics collection from a remote server via SSH
type RemoteStatsService struct {
	client *remote.SSHClient
}

// NewRemoteStatsService creates a new RemoteStatsService
func NewRemoteStatsService(client *remote.SSHClient) *RemoteStatsService {
	return &RemoteStatsService{client: client}
}

// GetSystemStats returns comprehensive system statistics from the remote server
func (s *RemoteStatsService) GetSystemStats(ctx context.Context) (*models.SystemStats, error) {
	// Execute commands concurrently or sequentially? Sequentially is safer for SSH session usage
	// 1. CPU (using top or mpstat if available, falling back to /proc/stat)
	// Simple parsing of /proc/stat
	cpuStats, err := s.getRemoteCPUStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("CPU metrics unavailable: %w", err)
	}

	// 2. Memory (free -b)
	memStats, err := s.getRemoteMemoryStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory metrics unavailable: %w", err)
	}

	// 3. Disk (df -B1)
	diskStats, err := s.getRemoteDiskStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("disk metrics unavailable: %w", err)
	}

	// 4. Network (/proc/net/dev)
	netStats, err := s.getRemoteNetworkStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("network metrics unavailable: %w", err)
	}

	// 5. Uptime (/proc/uptime)
	uptimeStr, err := s.client.RunCommandContext(ctx, "cat /proc/uptime | awk '{print $1}'")
	var uptime uint64
	if err == nil {
		val, _ := strconv.ParseFloat(strings.TrimSpace(uptimeStr), 64)
		uptime = uint64(val)
	}

	return &models.SystemStats{
		CPU:     *cpuStats,
		Memory:  *memStats,
		Disk:    diskStats,
		Network: *netStats,
		Uptime:  uptime,
	}, nil
}

// GetHostInfo returns general host information
func (s *RemoteStatsService) GetHostInfo(ctx context.Context) (map[string]interface{}, error) {
	output, err := s.client.RunCommandContext(ctx, "hostname && uname -s && uname -r && uname -m && uptime -s")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	info := make(map[string]interface{})
	if len(lines) >= 1 {
		info["hostname"] = lines[0]
	}
	if len(lines) >= 2 {
		info["os"] = lines[1]
	}
	if len(lines) >= 3 {
		info["kernel_version"] = lines[2]
	}
	if len(lines) >= 4 {
		info["kernel_arch"] = lines[3]
	}
	if len(lines) >= 5 {
		info["boot_time"] = lines[4] // This might need parsing to timestamp
	}

	return info, nil
}

func (s *RemoteStatsService) getRemoteCPUStats(ctx context.Context) (*models.CPUStats, error) {
	// Read Linux load and CPU counters. Never substitute a healthy zero on failure.
	output, err := s.client.RunCommandContext(ctx, "cat /proc/loadavg")
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(output)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid load average output")
	}
	loads := make([]float64, 3)
	for i := range loads {
		loads[i], err = strconv.ParseFloat(parts[i], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid load average")
		}
	}
	topOut, err := s.client.RunCommandContext(ctx, "LC_ALL=C top -bn1 | grep 'Cpu(s)'")
	if err != nil {
		return nil, err
	}
	percent, err := parseRemoteCPUPercent(topOut)
	if err != nil {
		return nil, err
	}
	l1, l5, l15 := loads[0], loads[1], loads[2]

	return &models.CPUStats{
		Percent:     percent,
		LoadAverage: []float64{l1, l5, l15},
	}, nil
}

func (s *RemoteStatsService) getRemoteMemoryStats(ctx context.Context) (*models.MemoryStats, error) {
	// free -b
	output, err := s.client.RunCommandContext(ctx, "free -b")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected free output")
	}

	// Mem:   total   used   free   shared  buff/cache   available
	fields := strings.Fields(lines[1])

	// Parse helper
	parseUint := func(s string) uint64 {
		v, _ := strconv.ParseUint(s, 10, 64)
		return v
	}

	if len(fields) >= 4 {
		total := parseUint(fields[1])
		used := parseUint(fields[2])
		free := parseUint(fields[3])

		var available uint64
		// available is usually the last one, or calculated
		if len(fields) >= 7 {
			var parseErr error
			available, parseErr = strconv.ParseUint(fields[6], 10, 64)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid available memory")
			}
		} else {
			available = free // fallback
		}

		if total == 0 || available > total {
			return nil, fmt.Errorf("invalid memory totals")
		}
		usedPercent := 0.0
		if total > 0 {
			usedPercent = (float64(total-available) / float64(total)) * 100
		}

		return &models.MemoryStats{
			Total:       total,
			Used:        used,
			Free:        free,
			Available:   available,
			UsedPercent: usedPercent,
		}, nil
	}

	return nil, fmt.Errorf("could not parse memory stats")
}

func (s *RemoteStatsService) getRemoteDiskStats(ctx context.Context) ([]models.DiskStats, error) {
	// df -B1
	output, err := s.client.RunCommandContext(ctx, "df -B1 -x squashfs -x iso9660 -x udf --output=source,target,fstype,size,used,avail,pcent | tail -n +2")
	if err != nil {
		return nil, err
	}

	var stats []models.DiskStats
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 7 {
			total, e1 := strconv.ParseUint(fields[3], 10, 64)
			used, e2 := strconv.ParseUint(fields[4], 10, 64)
			avail, e3 := strconv.ParseUint(fields[5], 10, 64)
			pcentStr := strings.TrimRight(fields[6], "%")
			pcent, e4 := strconv.ParseFloat(pcentStr, 64)
			if e1 != nil || e2 != nil || e3 != nil || e4 != nil || total == 0 || pcent < 0 || pcent > 100 {
				return nil, fmt.Errorf("invalid disk metrics")
			}

			stats = append(stats, models.DiskStats{
				Device:      fields[0],
				Mountpoint:  fields[1],
				Fstype:      fields[2],
				Total:       total,
				Used:        used,
				Free:        avail,
				UsedPercent: pcent,
			})
		}
	}
	if len(stats) == 0 {
		return nil, fmt.Errorf("no disk metrics returned")
	}
	return stats, nil
}

func (s *RemoteStatsService) getRemoteNetworkStats(ctx context.Context) (*models.NetworkStats, error) {
	// cat /proc/net/dev
	output, err := s.client.RunCommandContext(ctx, "cat /proc/net/dev")
	if err != nil {
		return nil, err
	}

	stats := &models.NetworkStats{}
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if i < 2 {
			continue
		} // skip headers
		parts := strings.Fields(line)
		if len(parts) >= 17 {
			// interface name is parts[0] (e.g. eth0:)
			// r_bytes: 1, r_packets: 2, r_errs: 3
			// t_bytes: 9, t_packets: 10, t_errs: 11

			rb, _ := strconv.ParseUint(parts[1], 10, 64)
			rp, _ := strconv.ParseUint(parts[2], 10, 64)
			re, _ := strconv.ParseUint(parts[3], 10, 64)

			tb, _ := strconv.ParseUint(parts[9], 10, 64)
			tp, _ := strconv.ParseUint(parts[10], 10, 64)
			te, _ := strconv.ParseUint(parts[11], 10, 64)

			stats.BytesRecv += rb
			stats.PacketsRecv += rp
			stats.ErrorsIn += re
			stats.BytesSent += tb
			stats.PacketsSent += tp
			stats.ErrorsOut += te
		}
	}
	return stats, nil
}

func parseRemoteCPUPercent(output string) (float64, error) {
	for _, part := range strings.Split(strings.TrimSpace(output), ",") {
		fields := strings.Fields(part)
		if len(fields) >= 2 && fields[len(fields)-1] == "id" {
			idle, err := strconv.ParseFloat(fields[len(fields)-2], 64)
			if err == nil && idle >= 0 && idle <= 100 {
				return 100 - idle, nil
			}
		}
	}
	return 0, fmt.Errorf("CPU idle percentage not present")
}
