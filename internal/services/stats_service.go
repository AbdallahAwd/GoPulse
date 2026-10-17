package services

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// LocalStatsService handles system statistics collection
type LocalStatsService struct{}

// NewLocalStatsService creates a new LocalStatsService
func NewLocalStatsService() *LocalStatsService {
	return &LocalStatsService{}
}

// GetSystemStats returns comprehensive system statistics
func (s *LocalStatsService) GetSystemStats(ctx context.Context) (*models.SystemStats, error) {
	cpuStats, err := s.GetCPUStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU stats: %w", err)
	}

	memStats, err := s.GetMemoryStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory stats: %w", err)
	}

	diskStats, err := s.GetDiskStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk stats: %w", err)
	}

	netStats, err := s.GetNetworkStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get network stats: %w", err)
	}

	uptime, err := host.Uptime()
	if err != nil {
		uptime = 0
	}

	return &models.SystemStats{
		CPU:     *cpuStats,
		Memory:  *memStats,
		Disk:    diskStats,
		Network: *netStats,
		Uptime:  uptime,
	}, nil
}

// GetCPUStats returns CPU usage statistics
func (s *LocalStatsService) GetCPUStats(ctx context.Context) (*models.CPUStats, error) {
	// Get overall CPU percentage
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil {
		return nil, err
	}

	var overallPercent float64
	if len(percentages) > 0 {
		overallPercent = percentages[0]
	}

	// Get per-CPU percentages
	perCPU, err := cpu.Percent(time.Second, true)
	if err != nil {
		perCPU = []float64{}
	}

	// Get CPU count
	coreCount, err := cpu.Counts(true)
	if err != nil {
		coreCount = 0
	}

	// Get load average
	loadAvg, err := load.Avg()
	var loadAvgSlice []float64
	if err == nil {
		loadAvgSlice = []float64{loadAvg.Load1, loadAvg.Load5, loadAvg.Load15}
	} else {
		loadAvgSlice = []float64{0, 0, 0}
	}

	return &models.CPUStats{
		Percent:     overallPercent,
		PerCPU:      perCPU,
		CoreCount:   coreCount,
		LoadAverage: loadAvgSlice,
	}, nil
}

// GetMemoryStats returns memory usage statistics
func (s *LocalStatsService) GetMemoryStats(ctx context.Context) (*models.MemoryStats, error) {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	return &models.MemoryStats{
		Total:       vmem.Total,
		Available:   vmem.Available,
		Used:        vmem.Used,
		UsedPercent: vmem.UsedPercent,
		Free:        vmem.Free,
		Cached:      vmem.Cached,
	}, nil
}

// GetDiskStats returns disk usage statistics for all partitions
func (s *LocalStatsService) GetDiskStats(ctx context.Context) ([]models.DiskStats, error) {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil, err
	}

	var result []models.DiskStats
	for _, partition := range partitions {
		if !monitorDiskPartition(partition.Fstype, slices.Contains(partition.Opts, "ro")) {
			continue
		}
		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			continue
		}

		result = append(result, models.DiskStats{
			Device:      partition.Device,
			Mountpoint:  partition.Mountpoint,
			Fstype:      partition.Fstype,
			Total:       usage.Total,
			Used:        usage.Used,
			Free:        usage.Free,
			UsedPercent: usage.UsedPercent,
		})
	}

	return result, nil
}

// GetNetworkStats returns network I/O statistics
func (s *LocalStatsService) GetNetworkStats(ctx context.Context) (*models.NetworkStats, error) {
	ioCounters, err := net.IOCounters(false)
	if err != nil {
		return nil, err
	}

	if len(ioCounters) == 0 {
		return &models.NetworkStats{}, nil
	}

	// Sum all interfaces
	stats := ioCounters[0]

	return &models.NetworkStats{
		BytesSent:   stats.BytesSent,
		BytesRecv:   stats.BytesRecv,
		PacketsSent: stats.PacketsSent,
		PacketsRecv: stats.PacketsRecv,
		ErrorsIn:    stats.Errin,
		ErrorsOut:   stats.Errout,
	}, nil
}

// GetHostInfo returns general host information
func (s *LocalStatsService) GetHostInfo(ctx context.Context) (map[string]interface{}, error) {
	info, err := host.Info()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"hostname":         info.Hostname,
		"os":               info.OS,
		"platform":         info.Platform,
		"platform_family":  info.PlatformFamily,
		"platform_version": info.PlatformVersion,
		"kernel_version":   info.KernelVersion,
		"kernel_arch":      info.KernelArch,
		"uptime":           info.Uptime,
		"boot_time":        info.BootTime,
	}, nil
}

// Read-only images (for example snap/squashfs and optical media) are normally
// 100% used and must not generate capacity alerts.
func monitorDiskPartition(filesystem string, readOnly bool) bool {
	if readOnly {
		return false
	}
	return !slices.Contains([]string{"squashfs", "iso9660", "udf", "devfs", "devtmpfs", "proc", "sysfs", "cgroup", "cgroup2", "autofs"}, filesystem)
}
