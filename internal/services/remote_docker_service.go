package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/remote"
)

// RemoteDockerService handles Docker operations on a remote server via SSH
type RemoteDockerService struct {
	client *remote.SSHClient
}

// NewRemoteDockerService creates a new RemoteDockerService
func NewRemoteDockerService(client *remote.SSHClient) *RemoteDockerService {
	return &RemoteDockerService{client: client}
}

// ListContainers returns all containers
func (s *RemoteDockerService) ListContainers(ctx context.Context) ([]models.ContainerInfo, error) {
	// docker ps -a --format '{{json .}}'
	// However, json format outputs one JSON object per line, which is not a valid JSON array.
	// We need to parse line by line.

	cmd := `docker ps -a --format '{"ID":"{{.ID}}","Names":"{{.Names}}","Image":"{{.Image}}","State":"{{.State}}","Status":"{{.Status}}","Ports":"{{.Ports}}","CreatedAt":"{{.CreatedAt}}","Labels":"{{.Labels}}"}'`
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containers = []models.ContainerInfo{}
	lines := strings.Split(strings.TrimSpace(output), "\n")

	type dockerPsOutput struct {
		ID        string `json:"ID"`
		Names     string `json:"Names"`
		Image     string `json:"Image"`
		State     string `json:"State"`
		Status    string `json:"Status"`
		Ports     string `json:"Ports"`
		CreatedAt string `json:"CreatedAt"`
		Labels    string `json:"Labels"`
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		var psOut dockerPsOutput
		if err := json.Unmarshal([]byte(line), &psOut); err != nil {
			// fmt.Printf("DEBUG: Failed to parse line: %s, error: %v\n", line, err)
			continue // Skip malformed lines
		}

		// Parse created time (format varies, usually "2023-01-01 10:00:00 +0000 UTC")
		// For simplicity, just use 0 or try basic parse if needed.
		// CreatedAt in Go format is tricky from docker output.

		ports := strings.Split(psOut.Ports, ", ")

		containers = append(containers, models.ContainerInfo{
			ID:      psOut.ID,
			Name:    psOut.Names,
			Image:   psOut.Image,
			State:   psOut.State,
			Status:  psOut.Status,
			Ports:   ports,
			Created: 0,                   // Placeholder
			Labels:  map[string]string{}, // Parsing labels string is feasible but skipping for brevity
		})
	}

	return containers, nil
}

// GetContainer returns specific container info
func (s *RemoteDockerService) GetContainer(ctx context.Context, id string) (*models.ContainerInfo, error) {
	// docker inspect id
	cmd := fmt.Sprintf("docker inspect %s", id)
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, err
	}

	// Output is a JSON array with one element
	var inspections []struct {
		Id    string
		Name  string
		State struct {
			Status  string
			Running bool
		}
		Config struct {
			Image  string
			Labels map[string]string
		}
		Created         string
		NetworkSettings struct {
			Ports map[string]interface{}
		}
	}

	if err := json.Unmarshal([]byte(output), &inspections); err != nil {
		return nil, fmt.Errorf("failed to parse inspect output: %w", err)
	}

	if len(inspections) == 0 {
		return nil, fmt.Errorf("container not found")
	}

	insp := inspections[0]

	var portList []string
	for p := range insp.NetworkSettings.Ports {
		portList = append(portList, p)
	}

	// Parse time
	createdTime, _ := time.Parse(time.RFC3339Nano, insp.Created)

	return &models.ContainerInfo{
		ID:      insp.Id[:12], // Short ID
		Name:    strings.TrimPrefix(insp.Name, "/"),
		Image:   insp.Config.Image,
		State:   insp.State.Status,
		Status:  insp.State.Status,
		Ports:   portList,
		Created: createdTime.Unix(),
		Labels:  insp.Config.Labels,
	}, nil
}

// GetContainerStats returns container stats
func (s *RemoteDockerService) GetContainerStats(ctx context.Context, id string) (*models.ContainerStats, error) {
	// docker stats --no-stream --format '{{json .}}' id
	// JSON format keys: BlockIO, CPUPerc, Container, ID, MemPerc, MemUsage, Name, NetIO, PIDs

	cmd := fmt.Sprintf("docker stats --no-stream --format '{\"CPUPerc\":\"{{.CPUPerc}}\",\"MemUsage\":\"{{.MemUsage}}\",\"NetIO\":\"{{.NetIO}}\"}' %s", id)
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, err
	}

	type statsOutput struct {
		CPUPerc  string `json:"CPUPerc"`
		MemUsage string `json:"MemUsage"`
		NetIO    string `json:"NetIO"`
	}

	var stats statsOutput
	if err := json.Unmarshal([]byte(output), &stats); err != nil {
		return nil, fmt.Errorf("failed to parse stats: %w", err)
	}

	// Parse fields
	// CPUPerc: "0.00%"
	cpuVal, _ := strconv.ParseFloat(strings.TrimRight(stats.CPUPerc, "%"), 64)

	// MemUsage: "10MiB / 1GiB"
	memParts := strings.Split(stats.MemUsage, "/")
	memUsageMB := 0.0
	memLimitMB := 0.0
	if len(memParts) >= 1 {
		memUsageMB = parseBytesToMB(strings.TrimSpace(memParts[0]))
	}
	if len(memParts) >= 2 {
		memLimitMB = parseBytesToMB(strings.TrimSpace(memParts[1]))
	}

	// NetIO: "1KB / 2KB"
	netParts := strings.Split(stats.NetIO, "/")
	netRxMB := 0.0
	netTxMB := 0.0
	if len(netParts) >= 2 {
		netRxMB = parseBytesToMB(strings.TrimSpace(netParts[0]))
		netTxMB = parseBytesToMB(strings.TrimSpace(netParts[1]))
	}

	return &models.ContainerStats{
		CPUPercent:    cpuVal,
		MemoryUsageMB: memUsageMB,
		MemoryLimitMB: memLimitMB,
		NetworkRxMB:   netRxMB,
		NetworkTxMB:   netTxMB,
	}, nil
}

func parseBytesToMB(s string) float64 {
	// Basic parsing: 100MB, 1GB, 100KB, 100B
	s = strings.ToUpper(s)
	unitMultipliers := map[string]float64{
		"B":   1.0 / (1024 * 1024),
		"KIB": 1.0 / 1024,
		"KB":  1.0 / 1000,
		"MIB": 1.0,
		"MB":  1.0, // treat as MiB for simplicity or 1000*1000
		"GIB": 1024.0,
		"GB":  1000.0,
	}

	for unit, mult := range unitMultipliers {
		if strings.HasSuffix(s, unit) {
			valStr := strings.TrimSuffix(s, unit)
			val, _ := strconv.ParseFloat(strings.TrimSpace(valStr), 64)
			return val * mult
		}
	}
	return 0
}

// GetContainerLogs returns logs
func (s *RemoteDockerService) GetContainerLogs(ctx context.Context, id string) ([]models.LogEntry, error) {
	cmd := fmt.Sprintf("docker logs --tail 100 %s", id)
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, err
	}

	var logs []models.LogEntry
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		logs = append(logs, models.LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Source:    "container",
			Message:   line,
		})
	}
	return logs, nil
}

func (s *RemoteDockerService) StartContainer(ctx context.Context, id string) error {
	_, err := s.client.RunCommand(fmt.Sprintf("docker start %s", id))
	return err
}

func (s *RemoteDockerService) StopContainer(ctx context.Context, id string) error {
	_, err := s.client.RunCommand(fmt.Sprintf("docker stop %s", id))
	return err
}

func (s *RemoteDockerService) RestartContainer(ctx context.Context, id string) error {
	_, err := s.client.RunCommand(fmt.Sprintf("docker restart %s", id))
	return err
}

func (s *RemoteDockerService) RemoveContainer(ctx context.Context, id string) error {
	_, err := s.client.RunCommand(fmt.Sprintf("docker rm %s", id))
	return err
}

// ListImages returns all images
func (s *RemoteDockerService) ListImages(ctx context.Context) ([]models.ImageInfo, error) {
	// docker images --format '{"ID":"{{.ID}}","Repository":"{{.Repository}}","Tag":"{{.Tag}}","Size":"{{.Size}}","CreatedSince":"{{.CreatedSince}}"}'
	// Note: RepoTags comes from Repository:Tag
	cmd := `docker images --format '{"ID":"{{.ID}}","Repository":"{{.Repository}}","Tag":"{{.Tag}}","Size":"{{.Size}}","CreatedSince":"{{.CreatedSince}}"}'`
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	var images []models.ImageInfo
	lines := strings.Split(strings.TrimSpace(output), "\n")
	type imageOut struct {
		ID           string `json:"ID"`
		Repository   string `json:"Repository"`
		Tag          string `json:"Tag"`
		Size         string `json:"Size"`
		CreatedSince string `json:"CreatedSince"`
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		var img imageOut
		if err := json.Unmarshal([]byte(line), &img); err != nil {
			continue
		}

		repoTag := fmt.Sprintf("%s:%s", img.Repository, img.Tag)
		// Size parsing (e.g. "100MB")
		size := int64(parseBytesToMB(img.Size) * 1024 * 1024)

		images = append(images, models.ImageInfo{
			ID:          img.ID,
			RepoTags:    []string{repoTag},
			Size:        size,
			Created:     0, // Parsing CreatedSince is hard, keeping simple for now
			VirtualSize: size,
		})
	}
	return images, nil
}

// PullImage pulls an image
func (s *RemoteDockerService) PullImage(ctx context.Context, imageName string) (io.ReadCloser, error) {
	cmd := fmt.Sprintf("docker pull %s", imageName)
	return s.client.StreamCommand(cmd)
}

// RemoveImage removes an image
func (s *RemoteDockerService) RemoveImage(ctx context.Context, id string) error {
	_, err := s.client.RunCommand(fmt.Sprintf("docker rmi %s", id))
	return err
}

// ListVolumes returns all volumes
func (s *RemoteDockerService) ListVolumes(ctx context.Context) ([]models.VolumeInfo, error) {
	cmd := `docker volume ls --format '{"Name":"{{.Name}}","Driver":"{{.Driver}}","Mountpoint":"{{.Mountpoint}}","Labels":"{{.Labels}}"}'`
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	var volumes []models.VolumeInfo
	lines := strings.Split(strings.TrimSpace(output), "\n")
	type volOut struct {
		Name       string `json:"Name"`
		Driver     string `json:"Driver"`
		Mountpoint string `json:"Mountpoint"`
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		var v volOut
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			continue
		}
		volumes = append(volumes, models.VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
		})
	}
	return volumes, nil
}

// RemoveVolume removes a volume
func (s *RemoteDockerService) RemoveVolume(ctx context.Context, name string) error {
	_, err := s.client.RunCommand(fmt.Sprintf("docker volume rm %s", name))
	return err
}

// ListNetworks returns all networks
func (s *RemoteDockerService) ListNetworks(ctx context.Context) ([]models.NetworkInfo, error) {
	// docker network ls --format '{{json .}}'
	// Removing Internal/Attachable as they are not reliably available in 'ls' format on all versions
	cmd := `docker network ls --format '{"ID":"{{.ID}}","Name":"{{.Name}}","Driver":"{{.Driver}}","Scope":"{{.Scope}}"}'`
	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	var networks []models.NetworkInfo
	lines := strings.Split(strings.TrimSpace(output), "\n")
	type netOut struct {
		ID     string `json:"ID"`
		Name   string `json:"Name"`
		Driver string `json:"Driver"`
		Scope  string `json:"Scope"`
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		var n netOut
		if err := json.Unmarshal([]byte(line), &n); err != nil {
			// fallback check?
			continue
		}

		networks = append(networks, models.NetworkInfo{
			ID:     n.ID,
			Name:   n.Name,
			Driver: n.Driver,
			Scope:  n.Scope,
		})
	}
	return networks, nil
}

// RemoveNetwork removes a network
func (s *RemoteDockerService) RemoveNetwork(ctx context.Context, id string) error {
	_, err := s.client.RunCommand(fmt.Sprintf("docker network rm %s", id))
	return err
}

// RunContainer runs a container from an image
func (s *RemoteDockerService) RunContainer(ctx context.Context, image string, name string, ports string) (string, error) {
	// docker run -d --name <name> -p <ports> <image>
	cmd := "docker run -d"
	if name != "" {
		cmd += fmt.Sprintf(" --name %s", name)
	}
	if ports != "" {
		cmd += fmt.Sprintf(" -p %s", ports)
	}
	cmd += fmt.Sprintf(" %s", image)

	output, err := s.client.RunCommand(cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// BuildImage builds a Docker image
func (s *RemoteDockerService) BuildImage(ctx context.Context, path string, tag string) (io.ReadCloser, error) {
	// Assume path is directory on remote server
	// cd path && docker build -t tag .
	cmd := fmt.Sprintf("cd %s && docker build -t %s .", path, tag)
	return s.client.StreamCommand(cmd)
}

// BuildCompose builds/runs a Docker Compose project
func (s *RemoteDockerService) BuildCompose(ctx context.Context, projectDir string) (io.ReadCloser, error) {
	// cd <dir> && docker compose up -d --build
	cmd := fmt.Sprintf("cd %s && docker compose up -d --build", projectDir)
	return s.client.StreamCommand(cmd)
}

func (s *RemoteDockerService) Close() error {
	return nil
}
