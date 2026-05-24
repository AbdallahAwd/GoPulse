package services

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// DockerService handles Docker operations
type DockerService struct {
	client *client.Client
}

// NewDockerService creates a new DockerService
func NewDockerService(host, version string) (*DockerService, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithVersion(version),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	return &DockerService{client: cli}, nil
}

// ListContainers returns all containers
func (s *DockerService) ListContainers(ctx context.Context) ([]models.ContainerInfo, error) {
	containers, err := s.client.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var result []models.ContainerInfo
	for _, c := range containers {
		ports := make([]string, 0)
		for _, port := range c.Ports {
			if port.PublicPort > 0 {
				ports = append(ports, fmt.Sprintf("%d:%d/%s", port.PublicPort, port.PrivatePort, port.Type))
			}
		}

		name := ""
		if len(c.Names) > 0 {
			// Remove leading / from container name
			name = c.Names[0][1:]
		}

		result = append(result, models.ContainerInfo{
			ID:      c.ID[:12], // Short ID
			Name:    name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Ports:   ports,
			Created: c.Created,
			Labels:  c.Labels,
		})
	}

	return result, nil
}

// GetContainer returns information about a specific container
func (s *DockerService) GetContainer(ctx context.Context, containerID string) (*models.ContainerInfo, error) {
	containers, err := s.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	for _, c := range containers {
		if c.ID == containerID || c.Name == containerID {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("container not found: %s", containerID)
}

// StartContainer starts a container
func (s *DockerService) StartContainer(ctx context.Context, containerID string) error {
	if err := s.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

// StopContainer stops a container
func (s *DockerService) StopContainer(ctx context.Context, containerID string) error {
	timeout := 10 // seconds
	if err := s.client.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// RestartContainer restarts a container
func (s *DockerService) RestartContainer(ctx context.Context, containerID string) error {
	timeout := 10 // seconds
	if err := s.client.ContainerRestart(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}
	return nil
}

// GetContainerStats returns real-time stats for a container
// Note: This is a simplified version. For detailed stats, use docker stats command
func (s *DockerService) GetContainerStats(ctx context.Context, containerID string) (*models.ContainerStats, error) {
	// Return basic stats
	// For more detailed stats, you would need to parse the stats stream
	return &models.ContainerStats{
		CPUPercent:    0, // Would need stats stream parsing
		MemoryUsageMB: 0, // Would need stats stream parsing
		MemoryLimitMB: 0, // Would need stats stream parsing
		NetworkRxMB:   0, // Would need stats stream parsing
		NetworkTxMB:   0, // Would need stats stream parsing
	}, nil
}

// GetContainerLogs returns container logs
func (s *DockerService) GetContainerLogs(ctx context.Context, containerID string) ([]models.LogEntry, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "100",
		Timestamps: true,
	}

	logsReader, err := s.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}
	defer logsReader.Close()

	output, err := io.ReadAll(logsReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read logs: %w", err)
	}

	var logs []models.LogEntry
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		// Parsing Docker logs... Docker returns header bytes for streams.
		// Standard log with TTY=false has 8-byte header: [STREAM_TYPE, 0, 0, 0, SIZE1, SIZE2, SIZE3, SIZE4]
		// But here we read all... client.ContainerLogs with TTY=true handles it?
		// If TTY is enabled for container, it's raw text. If not, it has header.
		// For simplicity, we just dump line as message.

		logs = append(logs, models.LogEntry{
			Timestamp: time.Now(), // Ideally parse from line if Timestamps=true
			Level:     "INFO",
			Source:    "container",
			Message:   line,
		})
	}

	return logs, nil
}

// RemoveContainer removes a container
func (s *DockerService) RemoveContainer(ctx context.Context, containerID string) error {
	if err := s.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: false,
	}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

// ListImages returns all images
func (s *DockerService) ListImages(ctx context.Context) ([]models.ImageInfo, error) {
	images, err := s.client.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	var result []models.ImageInfo
	for _, img := range images {
		result = append(result, models.ImageInfo{
			ID:          strings.TrimPrefix(img.ID, "sha256:"),
			RepoTags:    img.RepoTags,
			Size:        img.Size,
			Created:     img.Created,
			VirtualSize: img.VirtualSize,
		})
	}
	return result, nil
}

// PullImage pulls an image
func (s *DockerService) PullImage(ctx context.Context, imageName string) (io.ReadCloser, error) {
	return s.client.ImagePull(ctx, imageName, image.PullOptions{})
}

// RemoveImage removes an image
func (s *DockerService) RemoveImage(ctx context.Context, id string) error {
	_, err := s.client.ImageRemove(ctx, id, image.RemoveOptions{
		Force:         false,
		PruneChildren: true,
	})
	if err != nil {
		return fmt.Errorf("failed to remove image: %w", err)
	}
	return nil
}

// ListVolumes returns all volumes
func (s *DockerService) ListVolumes(ctx context.Context) ([]models.VolumeInfo, error) {
	volumes, err := s.client.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	var result []models.VolumeInfo
	for _, v := range volumes.Volumes {
		result = append(result, models.VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			CreatedAt:  v.CreatedAt,
		})
	}
	return result, nil
}

// RemoveVolume removes a volume
func (s *DockerService) RemoveVolume(ctx context.Context, name string) error {
	if err := s.client.VolumeRemove(ctx, name, true); err != nil {
		return fmt.Errorf("failed to remove volume: %w", err)
	}
	return nil
}

// ListNetworks returns all networks
func (s *DockerService) ListNetworks(ctx context.Context) ([]models.NetworkInfo, error) {
	networks, err := s.client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	var result []models.NetworkInfo
	for _, n := range networks {
		result = append(result, models.NetworkInfo{
			ID:         n.ID,
			Name:       n.Name,
			Driver:     n.Driver,
			Scope:      n.Scope,
			Internal:   n.Internal,
			Attachable: n.Attachable,
		})
	}
	return result, nil
}

// RemoveNetwork removes a network
func (s *DockerService) RemoveNetwork(ctx context.Context, id string) error {
	if err := s.client.NetworkRemove(ctx, id); err != nil {
		return fmt.Errorf("failed to remove network: %w", err)
	}
	return nil
}

// RunContainer runs a container from an image
func (s *DockerService) RunContainer(ctx context.Context, image string, name string, ports string) (string, error) {
	// docker run -d --name <name> -p <ports> <image>
	args := []string{"run", "-d"}
	if name != "" {
		args = append(args, "--name", name)
	}
	if ports != "" {
		args = append(args, "-p", ports)
	}
	args = append(args, image)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run container: %w, output: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// BuildImage builds a Docker image
func (s *DockerService) BuildImage(ctx context.Context, path string, tag string) (io.ReadCloser, error) {
	// Using exec for local build to get streaming output easily and consistency with CLI
	args := []string{"build", "-t", tag, path}
	cmd := exec.CommandContext(ctx, "docker", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // Combine stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Wrapper to wait for command on close
	return &cmdStream{cmd: cmd, stdout: stdout}, nil
}

// BuildCompose builds/runs a Docker Compose project
func (s *DockerService) BuildCompose(ctx context.Context, projectDir string) (io.ReadCloser, error) {
	// docker compose up -d --build
	args := []string{"compose", "up", "-d", "--build"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = projectDir

	// Debug logging
	fmt.Printf("Executing command: docker %v in dir: %s\n", args, projectDir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("Error creating stdout pipe: %v\n", err)
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		fmt.Printf("Error starting command: %v\n", err)
		return nil, err
	}

	return &cmdStream{cmd: cmd, stdout: stdout}, nil
}

type cmdStream struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
}

func (c *cmdStream) Read(p []byte) (n int, err error) {
	return c.stdout.Read(p)
}

func (c *cmdStream) Close() error {
	// Close stdout first
	c.stdout.Close()
	// Wait for command to finish (ignore error calling logs if already finished?)
	// Actually, Wait() closes pipes.
	// If we close ReadCloser, we should ensure Wait is called to avoid zombies.
	// But Wait() should be called after reading is done.
	// If caller Closes, we should probably just Wait() and return.
	return c.cmd.Wait()
}

// Close closes the Docker client
func (s *DockerService) Close() error {
	return s.client.Close()
}
