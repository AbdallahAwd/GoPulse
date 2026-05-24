package services

import (
	"context"
	"io"

	"github.com/amaa/gopulse/internal/models"
)

// StatsProvider defines the interface for collecting system statistics
type StatsProvider interface {
	GetSystemStats(ctx context.Context) (*models.SystemStats, error)
	GetHostInfo(ctx context.Context) (map[string]interface{}, error)
}

// ProcessProvider defines the interface for process management
type ProcessProvider interface {
	ListProcesses(ctx context.Context) ([]models.ProcessInfo, error)
	GetProcess(ctx context.Context, pid int32) (*models.ProcessInfo, error)
	KillProcess(ctx context.Context, pid int32) error
	RestartService(ctx context.Context, serviceName string) error
	GetServiceStatus(ctx context.Context, serviceName string) (string, error)
}

// DockerProvider defines the interface for Docker container management
type DockerProvider interface {
	// Containers
	ListContainers(ctx context.Context) ([]models.ContainerInfo, error)
	GetContainer(ctx context.Context, id string) (*models.ContainerInfo, error)
	GetContainerStats(ctx context.Context, id string) (*models.ContainerStats, error)
	GetContainerLogs(ctx context.Context, id string) ([]models.LogEntry, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error

	// Images
	ListImages(ctx context.Context) ([]models.ImageInfo, error)
	PullImage(ctx context.Context, image string) (io.ReadCloser, error)
	RemoveImage(ctx context.Context, id string) error

	// Volumes
	ListVolumes(ctx context.Context) ([]models.VolumeInfo, error)
	RemoveVolume(ctx context.Context, name string) error

	// Networks
	ListNetworks(ctx context.Context) ([]models.NetworkInfo, error)
	RemoveNetwork(ctx context.Context, id string) error

	// RunContainer runs a container from an image
	RunContainer(ctx context.Context, image string, name string, ports string) (string, error)

	// Build
	BuildImage(ctx context.Context, dockerfile string, tag string) (io.ReadCloser, error)
	BuildCompose(ctx context.Context, composeFile string) (io.ReadCloser, error)

	Close() error
}

// DatabaseProvider defines the interface for database management
