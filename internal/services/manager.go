package services

import (
	"fmt"
	"sync"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

// ServiceManager manages service providers for different servers
type ServiceManager struct {
	store      *store.ServerStore
	sshClients map[string]*remote.SSHClient
	mu         sync.RWMutex
	executor   CommandExecutor
}

// NewServiceManager creates a new ServiceManager
func NewServiceManager(store *store.ServerStore) *ServiceManager {
	return &ServiceManager{
		store:      store,
		sshClients: make(map[string]*remote.SSHClient),
	}
}

// GetStatsProvider returns a StatsProvider for the given server ID
func (m *ServiceManager) GetStatsProvider(serverID string) (StatsProvider, error) {
	server, err := m.store.GetServer(serverID)
	if err != nil {
		return nil, err
	}

	if server.Host == "localhost" || server.Host == "127.0.0.1" {
		return NewLocalStatsService(), nil
	}

	client, err := m.getSSHClient(server)
	if err != nil {
		return nil, err
	}

	return NewRemoteStatsService(client), nil
}

// GetProcessProvider returns a ProcessProvider for the given server ID
func (m *ServiceManager) GetProcessProvider(serverID string) (ProcessProvider, error) {
	server, err := m.store.GetServer(serverID)
	if err != nil {
		return nil, err
	}

	if server.Host == "localhost" || server.Host == "127.0.0.1" {
		// Local implementation currently in process_service.go
		return NewProcessService(), nil
	}

	client, err := m.getSSHClient(server)
	if err != nil {
		return nil, err
	}

	return NewRemoteProcessService(client), nil
}

// GetDockerProvider returns a DockerProvider for the given server ID
func (m *ServiceManager) GetDockerProvider(serverID string) (DockerProvider, error) {
	server, err := m.store.GetServer(serverID)
	if err != nil {
		return nil, err
	}

	if server.Host == "localhost" || server.Host == "127.0.0.1" {
		// Local implementation
		// We need Docker config here? Assuming default for now or we need to pass config to Manager
		// For simplicity, local docker uses default socket.
		return NewDockerService("unix:///var/run/docker.sock", "1.41")
	}

	client, err := m.getSSHClient(server)
	if err != nil {
		return nil, err
	}

	return NewRemoteDockerService(client), nil
}

// GetFileProvider returns a FileProvider (FileService) for the given server ID
func (m *ServiceManager) GetFileProvider(serverID string) (FileService, error) {
	server, err := m.store.GetServer(serverID)
	if err != nil {
		return nil, err
	}

	if server.Host == "localhost" || server.Host == "127.0.0.1" {
		// Local implementation could be added later using os package directly
		return nil, fmt.Errorf("local file management not implemented yet")
	}

	client, err := m.getSSHClient(server)
	if err != nil {
		return nil, err
	}

	return NewRemoteFileService(client)
}

func (m *ServiceManager) getSSHClient(server models.Server) (*remote.SSHClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, ok := m.sshClients[server.ID]
	if ok {
		return client, nil
	}

	newClient, err := remote.NewSSHClient(server)
	if err != nil {
		return nil, err
	}

	m.sshClients[server.ID] = newClient
	return newClient, nil
}

// GetSSHClient returns an SSH client for a specific server (internal/public usage)
func (m *ServiceManager) GetSSHClient(serverID string) (*remote.SSHClient, error) {
	server, err := m.store.GetServer(serverID)
	if err != nil {
		return nil, err
	}
	return m.getSSHClient(server)
}

// CloseAll closes all SSH connections
func (m *ServiceManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, client := range m.sshClients {
		client.Close()
	}
	m.sshClients = make(map[string]*remote.SSHClient)
}
