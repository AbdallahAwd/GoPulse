package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/remote"
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
	cpuStats, err := s.getRemoteCPUStats()
	if err != nil {
		fmt.Printf("Error getting CPU stats: %v\n", err)
		// Return empty stats rather than failing completely
		cpuStats = &models.CPUStats{}
	}

	// 2. Memory (free -b)
	memStats, err := s.getRemoteMemoryStats()
	if err != nil {
		fmt.Printf("Error getting Memory stats: %v\n", err)
		memStats = &models.MemoryStats{}
	}

	// 3. Disk (df -B1)
	diskStats, err := s.getRemoteDiskStats()
	if err != nil {
		fmt.Printf("Error getting Disk stats: %v\n", err)
		diskStats = []models.DiskStats{}
	}

	// 4. Network (/proc/net/dev)
	netStats, err := s.getRemoteNetworkStats()
	if err != nil {
		fmt.Printf("Error getting Network stats: %v\n", err)
		netStats = &models.NetworkStats{}
	}

	// 5. Uptime (/proc/uptime)
	uptimeStr, err := s.client.RunCommand("cat /proc/uptime | awk '{print $1}'")
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
	output, err := s.client.RunCommand("hostname && uname -s && uname -r && uname -m && uptime -s")
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

func (s *RemoteStatsService) getRemoteCPUStats() (*models.CPUStats, error) {
	// Read /proc/stat twice to calculate usage
	// Implementation simplified: just get load average for now
	output, err := s.client.RunCommand("cat /proc/loadavg")
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(output)
	if len(parts) < 3 {
		return &models.CPUStats{}, nil
	}

	l1, _ := strconv.ParseFloat(parts[0], 64)
	l5, _ := strconv.ParseFloat(parts[1], 64)
	l15, _ := strconv.ParseFloat(parts[2], 64)

	// To get percent, we'd need to sleep. For now, let's try top -bn1 | grep "Cpu(s)"
	// Or just return load average
	// TODO: Implement proper CPU percentage calculation by reading /proc/stat twice with delay
	// For MVP, Load Average is a good enough proxy if we can't reliably get %.

	// actually let's try top batch mode
	topOut, err := s.client.RunCommand("top -bn1 | grep 'Cpu(s)'")
	var percent float64
	if err == nil {
		// Example: %Cpu(s):  0.3 us,  0.2 sy,  0.0 ni, 99.5 id,  0.0 wa,  0.0 hi,  0.0 si,  0.0 st
		// 100 - idle = usage
		parts := strings.Split(topOut, ",")
		for _, p := range parts {
			if strings.Contains(p, "id") {
				idStr := strings.TrimSpace(strings.ReplaceAll(p, "id", ""))
				idle, _ := strconv.ParseFloat(idStr, 64)
				percent = 100 - idle
			}
		}
	}

	return &models.CPUStats{
		Percent:     percent,
		LoadAverage: []float64{l1, l5, l15},
	}, nil
}

func (s *RemoteStatsService) getRemoteMemoryStats() (*models.MemoryStats, error) {
	// free -b
	output, err := s.client.RunCommand("free -b")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected free output")
	}

	// Mem:   total   used   free   shared  buff/cache   available
	fields := strings.Fields(lines[1])
	if len(fields) < 7 {
		// Some versions might have fewer fields, try best effort
		// typically: total used free shared buff/cache available
	}

	// Parse helper
	parseUint := func(s string) uint64 {
		v, _ := strconv.ParseUint(s, 10, 64)
		return v
	}

	if len(fields) >= 2 {
		total := parseUint(fields[1])
		used := parseUint(fields[2])
		free := parseUint(fields[3])

		var available uint64
		// available is usually the last one, or calculated
		if len(fields) >= 7 {
			available = parseUint(fields[6])
		} else {
			available = free // fallback
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

func (s *RemoteStatsService) getRemoteDiskStats() ([]models.DiskStats, error) {
	// df -B1
	output, err := s.client.RunCommand("df -B1 --output=source,target,fstype,size,used,avail,pcent | tail -n +2")
	if err != nil {
		return nil, err
	}

	var stats []models.DiskStats
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 7 {
			total, _ := strconv.ParseUint(fields[3], 10, 64)
			used, _ := strconv.ParseUint(fields[4], 10, 64)
			avail, _ := strconv.ParseUint(fields[5], 10, 64)
			pcentStr := strings.TrimRight(fields[6], "%")
			pcent, _ := strconv.ParseFloat(pcentStr, 64)

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
	return stats, nil
}

func (s *RemoteStatsService) getRemoteNetworkStats() (*models.NetworkStats, error) {
	// cat /proc/net/dev
	output, err := s.client.RunCommand("cat /proc/net/dev")
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
