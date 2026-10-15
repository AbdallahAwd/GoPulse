package services

import (
	"context"
	"github.com/amaa/gopulse/internal/store"
	"regexp"
)

var safeResourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$`)

// CommandExecutor is an internal boundary for isolated integration tests. It is never exposed as an API/tool.
type CommandExecutor interface {
	Run(context.Context, string, string) (string, error)
}

func (m *ServiceManager) Run(ctx context.Context, serverID, command string) (string, error) {
	if m.executor != nil {
		return m.executor.Run(ctx, serverID, command)
	}
	client, err := m.GetSSHClient(serverID)
	if err != nil {
		return "", err
	}
	return client.RunCommandContext(ctx, command)
}

func NewServiceManagerWithExecutor(st *store.ServerStore, executor CommandExecutor) *ServiceManager {
	m := NewServiceManager(st)
	m.executor = executor
	return m
}
