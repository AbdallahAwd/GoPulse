package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var managedServiceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,127}$`)

func (m *ServiceManager) ManagedServiceStatus(ctx context.Context, server, name string) (string, error) {
	if !managedServiceName.MatchString(name) {
		return "", fmt.Errorf("invalid service name")
	}
	if m.executor == nil {
		srv, err := m.store.GetServer(server)
		if err != nil {
			return "", err
		}
		if srv.Host == "localhost" || srv.Host == "127.0.0.1" {
			return NewProcessService().GetServiceStatus(ctx, name)
		}
	}
	out, err := m.Run(ctx, server, "systemctl is-active -- "+shellQuote(name))
	status := strings.TrimSpace(out)
	switch status {
	case "active", "inactive", "failed", "activating", "deactivating":
		return status, nil
	}
	if err != nil {
		return "", fmt.Errorf("service status unavailable")
	}
	return "", fmt.Errorf("unexpected service status")
}
func (m *ServiceManager) RestartManagedService(ctx context.Context, server, name string) error {
	if !managedServiceName.MatchString(name) {
		return fmt.Errorf("invalid service name")
	}
	if m.executor == nil {
		srv, err := m.store.GetServer(server)
		if err != nil {
			return err
		}
		if srv.Host == "localhost" || srv.Host == "127.0.0.1" {
			if err := NewProcessService().RestartService(ctx, name); err != nil {
				return err
			}
			status, err := NewProcessService().GetServiceStatus(ctx, name)
			if err != nil || status != "active" {
				return fmt.Errorf("restart verification failed")
			}
			return nil
		}
	}
	if _, err := m.Run(ctx, server, "sudo -n systemctl restart -- "+shellQuote(name)); err != nil {
		return fmt.Errorf("service restart failed")
	}
	status, err := m.ManagedServiceStatus(ctx, server, name)
	if err != nil || status != "active" {
		return fmt.Errorf("service restart did not reach active state")
	}
	return nil
}
