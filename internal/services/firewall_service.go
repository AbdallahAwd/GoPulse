package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/remote"
	"github.com/amaa/gopulse/internal/store"
	"github.com/google/uuid"
)

// FirewallService manages VPS firewall rules remotely via SSH
type FirewallService struct {
	store          *store.FirewallStore
	serviceManager *ServiceManager
}

// NewFirewallService creates a new FirewallService instance
func NewFirewallService(store *store.FirewallStore, serviceManager *ServiceManager) *FirewallService {
	return &FirewallService{
		store:          store,
		serviceManager: serviceManager,
	}
}

// GetStore returns the underlying FirewallStore
func (s *FirewallService) GetStore() *store.FirewallStore {
	return s.store
}

// DetectBackend discovers the active/installed firewall backend on the remote host
func (s *FirewallService) DetectBackend(client *remote.SSHClient) (models.FirewallBackend, error) {
	// 1. Check UFW
	out, err := client.RunCommand("command -v ufw")
	if err == nil && strings.TrimSpace(out) != "" {
		return models.FirewallBackendUFW, nil
	}

	// 2. Check firewalld
	out, err = client.RunCommand("command -v firewall-cmd")
	if err == nil && strings.TrimSpace(out) != "" {
		return models.FirewallBackendFirewalld, nil
	}

	// 3. Check nftables
	out, err = client.RunCommand("command -v nft")
	if err == nil && strings.TrimSpace(out) != "" {
		return models.FirewallBackendNFTables, nil
	}

	// 4. Check iptables
	out, err = client.RunCommand("command -v iptables")
	if err == nil && strings.TrimSpace(out) != "" {
		return models.FirewallBackendIPTables, nil
	}

	return models.FirewallBackendNone, nil
}

// GetState returns the current real-time overview state of the firewall
func (s *FirewallService) GetState(serverID string, client *remote.SSHClient) (models.FirewallState, error) {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return models.FirewallState{Enabled: false, Backend: models.FirewallBackendNone}, err
	}

	state := models.FirewallState{
		Backend:               backend,
		Enabled:               false,
		DefaultIncomingPolicy: "unknown",
		DefaultOutgoingPolicy: "unknown",
		LastUpdated:           time.Now(),
	}

	if backend == models.FirewallBackendNone {
		return state, nil
	}

	// Read rule count from our SQLite DB cache
	rules, _ := s.store.ListRules(serverID)
	state.RulesCount = len(rules)

	switch backend {
	case models.FirewallBackendUFW:
		out, err := client.RunCommand("sudo ufw status verbose")
		if err != nil {
			// Try without sudo if root
			out, err = client.RunCommand("ufw status verbose")
		}

		if err == nil {
			if strings.Contains(out, "Status: active") {
				state.Enabled = true
			}
			// Parse Default Policies
			// e.g. "Default: deny (incoming), allow (outgoing), disabled (routed)"
			re := regexp.MustCompile(`Default:\s*([a-z]+)\s*\(incoming\),\s*([a-z]+)\s*\(outgoing\)`)
			matches := re.FindStringSubmatch(out)
			if len(matches) >= 3 {
				state.DefaultIncomingPolicy = matches[1]
				state.DefaultOutgoingPolicy = matches[2]
			}
		}

	case models.FirewallBackendFirewalld:
		out, err := client.RunCommand("sudo firewall-cmd --state")
		if err != nil {
			out, err = client.RunCommand("firewall-cmd --state")
		}
		if err == nil && strings.TrimSpace(out) == "running" {
			state.Enabled = true
		}

		// Parse Default policies from default zone target
		zoneOut, zoneErr := client.RunCommand("sudo firewall-cmd --zone=public --get-target")
		if zoneErr != nil {
			zoneOut, _ = client.RunCommand("firewall-cmd --zone=public --get-target")
		}
		target := strings.TrimSpace(zoneOut)
		if target == "default" || target == "ACCEPT" {
			state.DefaultIncomingPolicy = "allow"
		} else if target == "DROP" {
			state.DefaultIncomingPolicy = "deny"
		} else if target == "REJECT" {
			state.DefaultIncomingPolicy = "reject"
		} else {
			state.DefaultIncomingPolicy = target
		}
		state.DefaultOutgoingPolicy = "allow" // firewalld allows all egress by default

	case models.FirewallBackendIPTables, models.FirewallBackendNFTables:
		// Fallback/Legacy iptables checks
		out, err := client.RunCommand("sudo iptables -S")
		if err != nil {
			out, err = client.RunCommand("iptables -S")
		}
		if err == nil {
			state.Enabled = true // If iptables outputs rules, we consider it enabled
			// Parse policy rows, e.g. "-P INPUT ACCEPT"
			lines := strings.Split(out, "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "-P INPUT ") {
					parts := strings.Fields(line)
					if len(parts) >= 3 {
						state.DefaultIncomingPolicy = strings.ToLower(parts[2])
					}
				}
				if strings.HasPrefix(line, "-P OUTPUT ") {
					parts := strings.Fields(line)
					if len(parts) >= 3 {
						state.DefaultOutgoingPolicy = strings.ToLower(parts[2])
					}
				}
			}
		}
	}

	return state, nil
}

// EnableFirewall activates the firewall on the VPS
func (s *FirewallService) EnableFirewall(serverID string, client *remote.SSHClient, server models.Server) error {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return err
	}

	// Safety Check: Ensure SSH port is allowed before enabling
	if err := s.EnsureSSHAllowed(serverID, client, server); err != nil {
		return fmt.Errorf("lockout prevention check failed: %w", err)
	}

	var cmd string
	switch backend {
	case models.FirewallBackendUFW:
		cmd = "sudo ufw --force enable"
	case models.FirewallBackendFirewalld:
		cmd = "sudo systemctl enable firewalld && sudo systemctl start firewalld"
	case models.FirewallBackendIPTables:
		// For legacy iptables, "enabling" sets default policies to DROP (instead of ACCEPT)
		cmd = "sudo iptables -P INPUT DROP"
	default:
		return fmt.Errorf("no firewall package detected on this server")
	}

	_, err = client.RunCommand(cmd)
	if err != nil {
		// Try fallback command without sudo
		cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
		_, err = client.RunCommand(cmdNoSudo)
	}

	if err == nil {
		s.store.AddActivityLog(models.FirewallActivityLog{
			ID:        uuid.New().String(),
			ServerID:  serverID,
			Action:    "enable",
			Details:   "Firewall successfully enabled",
			Timestamp: time.Now(),
		})
	}
	return err
}

// DisableFirewall deactivates the firewall on the VPS
func (s *FirewallService) DisableFirewall(serverID string, client *remote.SSHClient) error {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return err
	}

	var cmd string
	switch backend {
	case models.FirewallBackendUFW:
		cmd = "sudo ufw disable"
	case models.FirewallBackendFirewalld:
		cmd = "sudo systemctl stop firewalld && sudo systemctl disable firewalld"
	case models.FirewallBackendIPTables:
		cmd = "sudo iptables -P INPUT ACCEPT"
	default:
		return fmt.Errorf("no firewall package detected on this server")
	}

	_, err = client.RunCommand(cmd)
	if err != nil {
		cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
		_, err = client.RunCommand(cmdNoSudo)
	}

	if err == nil {
		s.store.AddActivityLog(models.FirewallActivityLog{
			ID:        uuid.New().String(),
			ServerID:  serverID,
			Action:    "disable",
			Details:   "Firewall successfully disabled",
			Timestamp: time.Now(),
		})
	}
	return err
}

// ReloadFirewall reloads firewall configurations on the VPS
func (s *FirewallService) ReloadFirewall(serverID string, client *remote.SSHClient) error {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return err
	}

	var cmd string
	switch backend {
	case models.FirewallBackendUFW:
		cmd = "sudo ufw reload"
	case models.FirewallBackendFirewalld:
		cmd = "sudo firewall-cmd --reload"
	case models.FirewallBackendIPTables:
		// iptables-restore / apply saved rules
		cmd = "sudo iptables-save"
	default:
		return fmt.Errorf("reload not supported on this firewall backend")
	}

	_, err = client.RunCommand(cmd)
	if err != nil {
		cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
		_, err = client.RunCommand(cmdNoSudo)
	}

	if err == nil {
		s.store.AddActivityLog(models.FirewallActivityLog{
			ID:        uuid.New().String(),
			ServerID:  serverID,
			Action:    "reload",
			Details:   "Firewall configurations reloaded",
			Timestamp: time.Now(),
		})
	}
	return err
}

// ResetFirewall resets the firewall rules to default settings
func (s *FirewallService) ResetFirewall(serverID string, client *remote.SSHClient, server models.Server) error {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return err
	}

	// 1. Clear database rules
	if err := s.store.DeleteAllRules(serverID); err != nil {
		return err
	}

	// 2. Perform VPS reset
	var cmd string
	switch backend {
	case models.FirewallBackendUFW:
		cmd = "sudo ufw --force reset"
	case models.FirewallBackendFirewalld:
		// copy default zone xml back
		cmd = "sudo cp /usr/lib/firewalld/zones/public.xml /etc/firewalld/zones/public.xml && sudo firewall-cmd --reload"
	case models.FirewallBackendIPTables:
		cmd = "sudo iptables -F && sudo iptables -X && sudo iptables -P INPUT ACCEPT"
	default:
		return fmt.Errorf("reset not supported on this firewall backend")
	}

	_, err = client.RunCommand(cmd)
	if err != nil {
		cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
		_, err = client.RunCommand(cmdNoSudo)
	}

	// 3. Re-allow SSH automatically as a safety measure
	if err := s.EnsureSSHAllowed(serverID, client, server); err != nil {
		log.Printf("Reset warning: lockout safety check failed: %v", err)
	}

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "reset",
		Details:   "Firewall rules reset to defaults",
		Timestamp: time.Now(),
	})

	return nil
}

// ChangeDefaultPolicies updates the default policy for incoming and outgoing packets
func (s *FirewallService) ChangeDefaultPolicies(serverID string, client *remote.SSHClient, incoming, outgoing string) error {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return err
	}

	var cmds []string
	switch backend {
	case models.FirewallBackendUFW:
		cmds = append(cmds, fmt.Sprintf("sudo ufw default %s incoming", incoming))
		cmds = append(cmds, fmt.Sprintf("sudo ufw default %s outgoing", outgoing))
	case models.FirewallBackendFirewalld:
		// Target zone policy change
		targetMap := map[string]string{
			"allow":  "ACCEPT",
			"deny":   "DROP",
			"reject": "REJECT",
		}
		target := targetMap[incoming]
		if target != "" {
			cmds = append(cmds, fmt.Sprintf("sudo firewall-cmd --zone=public --set-target=%s --permanent", target))
			cmds = append(cmds, "sudo firewall-cmd --reload")
		}
	case models.FirewallBackendIPTables:
		policyMap := map[string]string{
			"allow":  "ACCEPT",
			"deny":   "DROP",
			"reject": "REJECT",
		}
		targetIn := policyMap[incoming]
		targetOut := policyMap[outgoing]
		if targetIn != "" {
			cmds = append(cmds, fmt.Sprintf("sudo iptables -P INPUT %s", targetIn))
		}
		if targetOut != "" {
			cmds = append(cmds, fmt.Sprintf("sudo iptables -P OUTPUT %s", targetOut))
		}
	default:
		return fmt.Errorf("policy modifications not supported on this backend")
	}

	for _, cmd := range cmds {
		_, err = client.RunCommand(cmd)
		if err != nil {
			cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
			_, err = client.RunCommand(cmdNoSudo)
		}
		if err != nil {
			return err
		}
	}

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "policy_change",
		Details:   fmt.Sprintf("Changed default policies: incoming=%s, outgoing=%s", incoming, outgoing),
		Timestamp: time.Now(),
	})
	return nil
}

// InstallUFW installs UFW on the VPS with one click and configures the connection SSH port
func (s *FirewallService) InstallUFW(serverID string, client *remote.SSHClient, server models.Server) error {
	cmd := fmt.Sprintf("sudo apt-get update && sudo apt-get install -y ufw || "+
		"sudo yum install -y ufw || "+
		"sudo dnf install -y ufw && "+
		"sudo ufw allow %d/tcp && "+
		"sudo ufw --force enable", server.Port)

	_, err := client.RunCommand(cmd)
	if err != nil {
		cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
		_, err = client.RunCommand(cmdNoSudo)
	}

	if err != nil {
		return err
	}

	// Auto populate rule list in GoPulse SQLite DB
	autoSSH := models.FirewallRule{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "allow",
		Direction: "ingress",
		Protocol:  "tcp",
		Port:      strconv.Itoa(server.Port),
		Source:    "any",
		Interface: "any",
		Enabled:   true,
		Comment:   "Auto-added SSH port on UFW installation",
		CreatedAt: time.Now(),
	}
	s.store.AddRule(autoSSH)

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "install",
		Details:   "Installed and activated UFW firewall",
		Timestamp: time.Now(),
	})

	return nil
}

// -----------------------------------------------------------------------------
// RULES ENGINE & ACTIONS
// -----------------------------------------------------------------------------

// ValidateRule checks port formatting, CIDR validities, etc.
func (s *FirewallService) ValidateRule(rule models.FirewallRule) error {
	if rule.Action != "allow" && rule.Action != "deny" && rule.Action != "reject" && rule.Action != "limit" {
		return fmt.Errorf("invalid action: %s", rule.Action)
	}
	if rule.Direction != "ingress" && rule.Direction != "egress" {
		return fmt.Errorf("invalid direction: %s", rule.Direction)
	}
	if rule.Protocol != "tcp" && rule.Protocol != "udp" && rule.Protocol != "icmp" && rule.Protocol != "any" {
		return fmt.Errorf("invalid protocol: %s", rule.Protocol)
	}

	// Port validation
	if rule.Port != "" && rule.Port != "any" {
		if strings.Contains(rule.Port, ":") {
			parts := strings.Split(rule.Port, ":")
			if len(parts) != 2 {
				return fmt.Errorf("invalid port range syntax: %s", rule.Port)
			}
			pStart, err1 := strconv.Atoi(parts[0])
			pEnd, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil || pStart < 1 || pStart > 65535 || pEnd < 1 || pEnd > 65535 || pStart > pEnd {
				return fmt.Errorf("invalid port range values: %s", rule.Port)
			}
		} else {
			p, err := strconv.Atoi(rule.Port)
			if err != nil || p < 1 || p > 65535 {
				return fmt.Errorf("invalid port: %s", rule.Port)
			}
		}
	}

	// Source validation
	if rule.Source != "" && rule.Source != "any" && rule.Source != "Anywhere" {
		if strings.Contains(rule.Source, "/") {
			_, _, err := net.ParseCIDR(rule.Source)
			if err != nil {
				return fmt.Errorf("invalid CIDR block: %s", rule.Source)
			}
		} else {
			ip := net.ParseIP(rule.Source)
			if ip == nil {
				return fmt.Errorf("invalid single IP address: %s", rule.Source)
			}
		}
	}

	return nil
}

// AddRule saves rule to DB and applies to VPS
func (s *FirewallService) AddRule(serverID string, rule models.FirewallRule, client *remote.SSHClient) error {
	if err := s.ValidateRule(rule); err != nil {
		return err
	}

	// Duplicate Rule Check
	existing, _ := s.store.ListRules(serverID)
	for _, r := range existing {
		if rulesMatch(r, rule) {
			return fmt.Errorf("duplicate rule exists (matches rule %s)", r.ID)
		}
	}

	// 1. Add to SQLite DB
	if err := s.store.AddRule(rule); err != nil {
		return err
	}

	// 2. Apply to VPS if enabled
	if rule.Enabled {
		if err := s.ApplyRule(client, rule); err != nil {
			// Rollback DB if VPS fails
			s.store.DeleteRule(rule.ID)
			return fmt.Errorf("failed to apply rule on VPS: %w", err)
		}
	}

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "add_rule",
		Details:   fmt.Sprintf("Added rule: %s %s port %s (%s)", rule.Action, rule.Direction, rule.Port, rule.Protocol),
		Timestamp: time.Now(),
	})

	return nil
}

// DeleteRule deletes rule from SQLite and removes it from VPS
func (s *FirewallService) DeleteRule(serverID string, ruleID string, client *remote.SSHClient) error {
	rule, err := s.store.GetRule(ruleID)
	if err != nil {
		return err
	}

	// Remove from VPS first if it was enabled
	if rule.Enabled {
		if err := s.RemoveRuleFromVPS(client, rule); err != nil {
			log.Printf("Warning: failed to delete rule directly from VPS: %v. Deleting from DB anyway.", err)
		}
	}

	// Delete from DB
	if err := s.store.DeleteRule(ruleID); err != nil {
		return err
	}

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "delete_rule",
		Details:   fmt.Sprintf("Deleted rule: %s %s port %s", rule.Action, rule.Direction, rule.Port),
		Timestamp: time.Now(),
	})

	return nil
}

// ToggleRule enables or disables an existing rule
func (s *FirewallService) ToggleRule(serverID string, ruleID string, enabled bool, client *remote.SSHClient) error {
	rule, err := s.store.GetRule(ruleID)
	if err != nil {
		return err
	}

	if rule.Enabled == enabled {
		return nil // No change
	}

	rule.Enabled = enabled

	// Sync with VPS
	if enabled {
		if err := s.ApplyRule(client, rule); err != nil {
			return err
		}
	} else {
		if err := s.RemoveRuleFromVPS(client, rule); err != nil {
			return err
		}
	}

	// Update DB
	if err := s.store.UpdateRule(rule); err != nil {
		return err
	}

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "toggle_rule",
		Details:   fmt.Sprintf("Toggled rule (enabled=%t): %s port %s", enabled, rule.Action, rule.Port),
		Timestamp: time.Now(),
	})

	return nil
}

// EditRule updates rule parameters in DB and updates active state on VPS
func (s *FirewallService) EditRule(serverID string, oldRule models.FirewallRule, newRule models.FirewallRule, client *remote.SSHClient) error {
	if err := s.ValidateRule(newRule); err != nil {
		return err
	}

	// Remove old rule from VPS if enabled
	if oldRule.Enabled {
		s.RemoveRuleFromVPS(client, oldRule)
	}

	// Push new rule to VPS if enabled
	if newRule.Enabled {
		if err := s.ApplyRule(client, newRule); err != nil {
			// Rollback to old rule on VPS if it failed
			if oldRule.Enabled {
				s.ApplyRule(client, oldRule)
			}
			return fmt.Errorf("failed to apply updated rule on VPS: %w", err)
		}
	}

	// Save to DB
	if err := s.store.UpdateRule(newRule); err != nil {
		return err
	}

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "edit_rule",
		Details:   fmt.Sprintf("Updated rule parameters for rule %s", newRule.ID),
		Timestamp: time.Now(),
	})

	return nil
}

// EnsureSSHAllowed checks if SSH connection is allowed, adding rule if missing
func (s *FirewallService) EnsureSSHAllowed(serverID string, client *remote.SSHClient, server models.Server) error {
	rules, err := s.store.ListRules(serverID)
	if err != nil {
		return err
	}

	sshPortStr := strconv.Itoa(server.Port)
	sshAllowed := false
	for _, rule := range rules {
		if rule.Enabled && rule.Action == "allow" && rule.Direction == "ingress" &&
			(rule.Protocol == "tcp" || rule.Protocol == "any") &&
			(rule.Port == sshPortStr || rule.Port == "any") &&
			(rule.Source == "any" || rule.Source == "Anywhere" || rule.Source == "") {
			sshAllowed = true
			break
		}
	}

	if !sshAllowed {
		// Automatically create and apply SSH safety rule
		sshRule := models.FirewallRule{
			ID:        uuid.New().String(),
			ServerID:  serverID,
			Action:    "allow",
			Direction: "ingress",
			Protocol:  "tcp",
			Port:      sshPortStr,
			Source:    "any",
			Interface: "any",
			Enabled:   true,
			Comment:   "Auto-generated lockout prevention rule",
			CreatedAt: time.Now(),
		}

		if err := s.store.AddRule(sshRule); err != nil {
			return err
		}

		if err := s.ApplyRule(client, sshRule); err != nil {
			s.store.DeleteRule(sshRule.ID)
			return err
		}

		s.store.AddActivityLog(models.FirewallActivityLog{
			ID:        uuid.New().String(),
			ServerID:  serverID,
			Action:    "add_rule",
			Details:   fmt.Sprintf("Auto-added connection safety rule for SSH port %s", sshPortStr),
			Timestamp: time.Now(),
		})
	}

	return nil
}

// ApplyRule builds the command and executes it on the VPS firewall to ALLOW/DENY/etc
func (s *FirewallService) ApplyRule(client *remote.SSHClient, rule models.FirewallRule) error {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return err
	}

	var cmd string
	switch backend {
	case models.FirewallBackendUFW:
		cmd = s.buildUFWCommand(rule, "add")
	case models.FirewallBackendFirewalld:
		cmd = s.buildFirewalldCommand(rule, "add")
	case models.FirewallBackendIPTables:
		cmd = s.buildIPTablesCommand(rule, "add")
	default:
		return fmt.Errorf("applying rules is not supported on this firewall backend")
	}

	_, err = client.RunCommand(cmd)
	if err != nil {
		cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
		_, err = client.RunCommand(cmdNoSudo)
	}
	return err
}

// RemoveRuleFromVPS builds deletion command and runs on the VPS
func (s *FirewallService) RemoveRuleFromVPS(client *remote.SSHClient, rule models.FirewallRule) error {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return err
	}

	var cmd string
	switch backend {
	case models.FirewallBackendUFW:
		cmd = s.buildUFWCommand(rule, "delete")
	case models.FirewallBackendFirewalld:
		cmd = s.buildFirewalldCommand(rule, "delete")
	case models.FirewallBackendIPTables:
		cmd = s.buildIPTablesCommand(rule, "delete")
	default:
		return fmt.Errorf("deleting rules is not supported on this firewall backend")
	}

	_, err = client.RunCommand(cmd)
	if err != nil {
		cmdNoSudo := strings.ReplaceAll(cmd, "sudo ", "")
		_, err = client.RunCommand(cmdNoSudo)
	}
	return err
}

func (s *FirewallService) buildUFWCommand(rule models.FirewallRule, operation string) string {
	var cmd strings.Builder
	cmd.WriteString("sudo ufw ")
	if operation == "delete" {
		cmd.WriteString("delete ")
	}

	if rule.Action == "limit" {
		cmd.WriteString("limit ")
	} else {
		cmd.WriteString(rule.Action + " ")
	}

	if rule.Direction == "egress" {
		cmd.WriteString("out ")
	} else {
		cmd.WriteString("in ")
	}

	if rule.Interface != "" && rule.Interface != "any" {
		cmd.WriteString("on " + rule.Interface + " ")
	}

	if rule.Protocol != "" && rule.Protocol != "any" {
		cmd.WriteString("proto " + rule.Protocol + " ")
	}

	if rule.Source != "" && rule.Source != "any" && rule.Source != "Anywhere" {
		cmd.WriteString("from " + rule.Source + " ")
	} else {
		cmd.WriteString("from any ")
	}

	cmd.WriteString("to any ")

	if rule.Port != "" && rule.Port != "any" {
		cmd.WriteString("port " + rule.Port)
	}

	return strings.TrimSpace(cmd.String())
}

func (s *FirewallService) buildFirewalldCommand(rule models.FirewallRule, operation string) string {
	// firewalld uses rich-rules
	var cmd strings.Builder
	cmd.WriteString("sudo firewall-cmd --permanent --zone=public ")
	if operation == "delete" {
		cmd.WriteString("--remove-rich-rule=")
	} else {
		cmd.WriteString("--add-rich-rule=")
	}

	var rich strings.Builder
	rich.WriteString("rule family=\"ipv4\" ")

	if rule.Source != "" && rule.Source != "any" && rule.Source != "Anywhere" {
		rich.WriteString(fmt.Sprintf("source address=\"%s\" ", rule.Source))
	}

	if rule.Port != "" && rule.Port != "any" {
		proto := rule.Protocol
		if proto == "" || proto == "any" {
			proto = "tcp" // default to tcp in firewalld
		}
		rich.WriteString(fmt.Sprintf("port port=\"%s\" protocol=\"%s\" ", rule.Port, proto))
	}

	action := rule.Action
	if action == "allow" {
		action = "accept"
	} else if action == "deny" {
		action = "drop"
	} // reject is reject

	rich.WriteString(action)

	cmd.WriteString(fmt.Sprintf("'%s'", rich.String()))
	return cmd.String()
}

func (s *FirewallService) buildIPTablesCommand(rule models.FirewallRule, operation string) string {
	var cmd strings.Builder
	cmd.WriteString("sudo iptables ")
	if operation == "delete" {
		cmd.WriteString("-D ")
	} else {
		cmd.WriteString("-A ")
	}

	if rule.Direction == "egress" {
		cmd.WriteString("OUTPUT ")
	} else {
		cmd.WriteString("INPUT ")
	}

	if rule.Interface != "" && rule.Interface != "any" {
		cmd.WriteString("-i " + rule.Interface + " ")
	}

	if rule.Protocol != "" && rule.Protocol != "any" {
		cmd.WriteString("-p " + rule.Protocol + " ")
	}

	if rule.Source != "" && rule.Source != "any" && rule.Source != "Anywhere" {
		cmd.WriteString("-s " + rule.Source + " ")
	}

	if rule.Port != "" && rule.Port != "any" {
		cmd.WriteString("--dport " + rule.Port + " ")
	}

	target := "ACCEPT"
	if rule.Action == "deny" {
		target = "DROP"
	} else if rule.Action == "reject" {
		target = "REJECT"
	}

	cmd.WriteString("-j " + target)
	return strings.TrimSpace(cmd.String())
}

// -----------------------------------------------------------------------------
// REMOTE PARSERS (import rules from VPS)
// -----------------------------------------------------------------------------

// GetRulesFromVPS executes list rules command and parses output
func (s *FirewallService) GetRulesFromVPS(client *remote.SSHClient, backend models.FirewallBackend, serverID string) ([]models.FirewallRule, error) {
	var rules []models.FirewallRule

	switch backend {
	case models.FirewallBackendUFW:
		out, err := client.RunCommand("sudo ufw status numbered")
		if err != nil {
			out, err = client.RunCommand("ufw status numbered")
		}
		if err != nil {
			return nil, err
		}
		rules = s.parseUFWStatus(out, serverID)

	case models.FirewallBackendFirewalld:
		out, err := client.RunCommand("sudo firewall-cmd --list-all")
		if err != nil {
			out, err = client.RunCommand("firewall-cmd --list-all")
		}
		if err != nil {
			return nil, err
		}
		rules = s.parseFirewalldListAll(out, serverID)

	case models.FirewallBackendIPTables, models.FirewallBackendNFTables:
		out, err := client.RunCommand("sudo iptables -S")
		if err != nil {
			out, err = client.RunCommand("iptables -S")
		}
		if err != nil {
			return nil, err
		}
		rules = s.parseIPTablesStatus(out, serverID)
	}

	return rules, nil
}

func (s *FirewallService) parseUFWStatus(output string, serverID string) []models.FirewallRule {
	var rules []models.FirewallRule
	lines := strings.Split(output, "\n")

	// Regex to match rule lines: "[ 1] 80/tcp on eth0             ALLOW IN    Anywhere"
	reRule := regexp.MustCompile(`\[\s*\d+\]\s+(.+?)\s+(ALLOW|DENY|REJECT|LIMIT)\s+(IN|OUT)\s+(.+)$`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		matches := reRule.FindStringSubmatch(line)
		if len(matches) >= 5 {
			toVal := matches[1]
			actionStr := strings.ToLower(matches[2])
			dirStr := strings.ToLower(matches[3])
			fromVal := matches[4]

			port := "any"
			proto := "any"
			intf := "any"

			// Parse Interface "on eth0"
			// E.g. "80/tcp on eth0"
			if strings.Contains(toVal, " on ") {
				parts := strings.Split(toVal, " on ")
				toVal = parts[0]
				intf = parts[1]
			}

			// Parse Port & Protocol "80/tcp"
			if strings.Contains(toVal, "/") {
				parts := strings.Split(toVal, "/")
				port = parts[0]
				proto = parts[1]
			} else if toVal != "Anywhere" && toVal != "any" {
				// Pure port
				port = toVal
			}

			direction := "ingress"
			if dirStr == "out" {
				direction = "egress"
			}

			rules = append(rules, models.FirewallRule{
				ID:        uuid.New().String(),
				ServerID:  serverID,
				Action:    actionStr,
				Direction: direction,
				Protocol:  proto,
				Port:      port,
				Source:    fromVal,
				Interface: intf,
				Enabled:   true,
				Comment:   "Imported from UFW status",
				CreatedAt: time.Now(),
			})
		}
	}
	return rules
}

func (s *FirewallService) parseFirewalldListAll(output string, serverID string) []models.FirewallRule {
	var rules []models.FirewallRule
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse simple ports e.g. "ports: 80/tcp 443/tcp"
		if strings.HasPrefix(line, "ports:") {
			portsStr := strings.TrimPrefix(line, "ports:")
			ports := strings.Fields(portsStr)
			for _, p := range ports {
				parts := strings.Split(p, "/")
				if len(parts) == 2 {
					rules = append(rules, models.FirewallRule{
						ID:        uuid.New().String(),
						ServerID:  serverID,
						Action:    "allow",
						Direction: "ingress",
						Protocol:  parts[1],
						Port:      parts[0],
						Source:    "any",
						Interface: "any",
						Enabled:   true,
						Comment:   "Imported simple port",
						CreatedAt: time.Now(),
					})
				}
			}
		}

		// Parse rich-rules
		// E.g. "rich rules: rule family="ipv4" source address="1.2.3.4" port port="22" protocol="tcp" accept"
		if strings.Contains(line, "rule ") {
			// Extract details using regex
			reSource := regexp.MustCompile(`source address="([^"]+)"`)
			rePort := regexp.MustCompile(`port port="([^"]+)"`)
			reProto := regexp.MustCompile(`protocol="([^"]+)"`)
			reAction := regexp.MustCompile(`(accept|drop|reject)`)

			src := "any"
			port := "any"
			proto := "any"
			action := "allow"

			if m := reSource.FindStringSubmatch(line); len(m) >= 2 {
				src = m[1]
			}
			if m := rePort.FindStringSubmatch(line); len(m) >= 2 {
				port = m[1]
			}
			if m := reProto.FindStringSubmatch(line); len(m) >= 2 {
				proto = m[1]
			}
			if m := reAction.FindStringSubmatch(line); len(m) >= 2 {
				actVal := m[1]
				if actVal == "accept" {
					action = "allow"
				} else if actVal == "drop" {
					action = "deny"
				} else {
					action = "reject"
				}
			}

			rules = append(rules, models.FirewallRule{
				ID:        uuid.New().String(),
				ServerID:  serverID,
				Action:    action,
				Direction: "ingress",
				Protocol:  proto,
				Port:      port,
				Source:    src,
				Interface: "any",
				Enabled:   true,
				Comment:   "Imported firewalld rich rule",
				CreatedAt: time.Now(),
			})
		}
	}
	return rules
}

func (s *FirewallService) parseIPTablesStatus(output string, serverID string) []models.FirewallRule {
	var rules []models.FirewallRule
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-A ") {
			continue // Only parse rule definitions
		}

		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}

		chain := parts[1] // INPUT or OUTPUT
		if chain != "INPUT" && chain != "OUTPUT" {
			continue // Only ingress/egress
		}

		direction := "ingress"
		if chain == "OUTPUT" {
			direction = "egress"
		}

		action := "allow"
		port := "any"
		proto := "any"
		src := "any"
		intf := "any"

		// Parse argument options
		for i := 2; i < len(parts); i++ {
			switch parts[i] {
			case "-p":
				if i+1 < len(parts) {
					proto = parts[i+1]
					i++
				}
			case "-s":
				if i+1 < len(parts) {
					src = parts[i+1]
					i++
				}
			case "-i":
				if i+1 < len(parts) {
					intf = parts[i+1]
					i++
				}
			case "--dport":
				if i+1 < len(parts) {
					port = parts[i+1]
					i++
				}
			case "-j":
				if i+1 < len(parts) {
					tgt := parts[i+1]
					if tgt == "DROP" {
						action = "deny"
					} else if tgt == "REJECT" {
						action = "reject"
					}
					i++
				}
			}
		}

		rules = append(rules, models.FirewallRule{
			ID:        uuid.New().String(),
			ServerID:  serverID,
			Action:    action,
			Direction: direction,
			Protocol:  proto,
			Port:      port,
			Source:    src,
			Interface: intf,
			Enabled:   true,
			Comment:   "Imported from iptables -S",
			CreatedAt: time.Now(),
		})
	}

	return rules
}

func rulesMatch(r1, r2 models.FirewallRule) bool {
	return r1.Action == r2.Action &&
		r1.Direction == r2.Direction &&
		r1.Protocol == r2.Protocol &&
		r1.Port == r2.Port &&
		r1.Source == r2.Source &&
		r1.Interface == r2.Interface
}

// -----------------------------------------------------------------------------
// SCHEDULER BACKGROUND ENGINE
// -----------------------------------------------------------------------------

// StartScheduler runs a background ticker processing scheduled changes
func (s *FirewallService) StartScheduler(ctx context.Context, serverStore *store.ServerStore) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processPendingSchedules(serverStore)
		}
	}
}

func (s *FirewallService) processPendingSchedules(serverStore *store.ServerStore) {
	schedules, err := s.store.ListPendingSchedules()
	if err != nil || len(schedules) == 0 {
		return
	}

	for _, sched := range schedules {
		log.Printf("Executing scheduled firewall task %s for server %s...", sched.ID, sched.ServerID)

		_, err = serverStore.GetServer(sched.ServerID)
		if err != nil {
			s.markScheduleFailed(sched, fmt.Sprintf("server load failure: %v", err))
			continue
		}

		client, err := s.serviceManager.GetSSHClient(sched.ServerID)
		if err != nil {
			s.markScheduleFailed(sched, fmt.Sprintf("SSH connection failure: %v", err))
			continue
		}

		var rule models.FirewallRule
		if err := json.Unmarshal([]byte(sched.RuleData), &rule); err != nil {
			s.markScheduleFailed(sched, fmt.Sprintf("JSON decode error: %v", err))
			continue
		}

		var runErr error
		switch sched.ActionType {
		case "add":
			runErr = s.AddRule(sched.ServerID, rule, client)
		case "delete":
			runErr = s.DeleteRule(sched.ServerID, rule.ID, client)
		case "enable":
			runErr = s.ToggleRule(sched.ServerID, rule.ID, true, client)
		case "disable":
			runErr = s.ToggleRule(sched.ServerID, rule.ID, false, client)
		default:
			runErr = fmt.Errorf("unknown action type: %s", sched.ActionType)
		}

		if runErr != nil {
			s.markScheduleFailed(sched, runErr.Error())
		} else {
			sched.Executed = true
			sched.Status = "success"
			sched.ErrorMessage = ""
			s.store.UpdateSchedule(sched)
		}
	}
}

func (s *FirewallService) markScheduleFailed(sched models.FirewallSchedule, errStr string) {
	sched.Executed = true
	sched.Status = "failed"
	sched.ErrorMessage = errStr
	s.store.UpdateSchedule(sched)

	s.store.AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  sched.ServerID,
		Action:    "schedule_failed",
		Details:   fmt.Sprintf("Scheduled change failed: %s", errStr),
		Timestamp: time.Now(),
	})
}

// SyncFromVPS queries active rules from the VPS firewall, matches/caches them in local SQLite DB
func (s *FirewallService) SyncFromVPS(serverID string, client *remote.SSHClient) ([]models.FirewallRule, error) {
	backend, err := s.DetectBackend(client)
	if err != nil {
		return nil, err
	}

	// 1. Get real rules from VPS
	vpsRules, err := s.GetRulesFromVPS(client, backend, serverID)
	if err != nil {
		return nil, err
	}

	// 2. Get rules from SQLite DB
	dbRules, err := s.store.ListRules(serverID)
	if err != nil {
		return nil, err
	}

	matchedDBIds := make(map[string]bool)

	// Update DB rules based on VPS state
	for _, vr := range vpsRules {
		found := false
		for _, dr := range dbRules {
			if rulesMatch(vr, dr) {
				found = true
				matchedDBIds[dr.ID] = true
				// Sync enabled status: it is on VPS, so must be enabled in DB
				if !dr.Enabled {
					dr.Enabled = true
					s.store.UpdateRule(dr)
				}
				break
			}
		}
		if !found {
			// Rule exists on VPS but not in GoPulse DB. Add it as enabled.
			vr.Enabled = true
			s.store.AddRule(vr)
		}
	}

	// Set rules to disabled in DB if they do not exist on the VPS
	for _, dr := range dbRules {
		if !matchedDBIds[dr.ID] && dr.Enabled {
			dr.Enabled = false
			s.store.UpdateRule(dr)
		}
	}

	return s.store.ListRules(serverID)
}

