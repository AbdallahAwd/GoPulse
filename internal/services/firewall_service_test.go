package services

import (
	"testing"
)

func TestParseUFWStatus(t *testing.T) {
	// Dummy FirewallService
	svc := &FirewallService{}

	ufwOutput := `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 80/tcp on eth0             ALLOW IN    Anywhere
[ 3] 443/udp                    DENY IN     1.2.3.4
[ 4] 10000:20000/tcp            LIMIT IN    Anywhere
`

	rules := svc.parseUFWStatus(ufwOutput, "srv-1")

	if len(rules) != 4 {
		t.Fatalf("expected 4 parsed rules, got %d", len(rules))
	}

	// Test rule 1: SSH allow
	r1 := rules[0]
	if r1.Port != "22" || r1.Protocol != "tcp" || r1.Action != "allow" || r1.Direction != "ingress" || r1.Source != "Anywhere" {
		t.Errorf("rule 1 mismatch: %+v", r1)
	}

	// Test rule 2: HTTP on eth0
	r2 := rules[1]
	if r2.Port != "80" || r2.Protocol != "tcp" || r2.Action != "allow" || r2.Interface != "eth0" {
		t.Errorf("rule 2 mismatch: %+v", r2)
	}

	// Test rule 3: HTTPS deny from source
	r3 := rules[2]
	if r3.Port != "443" || r3.Protocol != "udp" || r3.Action != "deny" || r3.Source != "1.2.3.4" {
		t.Errorf("rule 3 mismatch: %+v", r3)
	}

	// Test rule 4: Port range limit
	r4 := rules[3]
	if r4.Port != "10000:20000" || r4.Protocol != "tcp" || r4.Action != "limit" {
		t.Errorf("rule 4 mismatch: %+v", r4)
	}
}

func TestParseFirewalldListAll(t *testing.T) {
	svc := &FirewallService{}

	firewalldOutput := `public (active)
  target: default
  icmp-block-inversion: no
  interfaces: eth0
  sources: 
  services: ssh dhcpv6-client
  ports: 80/tcp 443/udp
  protocols: 
  forward-ports: 
  source-ports: 
  icmp-blocks: 
  rich rules: 
	rule family="ipv4" source address="1.2.3.4" port port="3306" protocol="tcp" accept
	rule family="ipv4" port port="6379" protocol="tcp" drop
`

	rules := svc.parseFirewalldListAll(firewalldOutput, "srv-1")

	// We expect 80/tcp, 443/udp, rich-rule-3306, rich-rule-6379 -> total 4 rules
	// Note: We don't map default services like ssh/dhcpv6-client unless we specifically add support,
	// but the parser extracts simple ports and rich rules.
	if len(rules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(rules))
	}

	r1 := rules[0] // 80/tcp
	if r1.Port != "80" || r1.Protocol != "tcp" || r1.Action != "allow" {
		t.Errorf("r1 mismatch: %+v", r1)
	}

	r2 := rules[1] // 443/udp
	if r2.Port != "443" || r2.Protocol != "udp" || r2.Action != "allow" {
		t.Errorf("r2 mismatch: %+v", r2)
	}

	r3 := rules[2] // Rich rule 3306
	if r3.Port != "3306" || r3.Protocol != "tcp" || r3.Source != "1.2.3.4" || r3.Action != "allow" {
		t.Errorf("r3 mismatch: %+v", r3)
	}

	r4 := rules[3] // Rich rule 6379
	if r4.Port != "6379" || r4.Protocol != "tcp" || r4.Action != "deny" {
		t.Errorf("r4 mismatch: %+v", r4)
	}
}

func TestParseIPTablesStatus(t *testing.T) {
	svc := &FirewallService{}

	iptablesOutput := `-P INPUT ACCEPT
-P FORWARD ACCEPT
-P OUTPUT ACCEPT
-A INPUT -i eth0 -p tcp -m tcp --dport 22 -j ACCEPT
-A INPUT -p tcp -m tcp -s 192.168.1.100 --dport 80 -j DROP
-A OUTPUT -p udp -m udp --dport 53 -j ACCEPT
`

	rules := svc.parseIPTablesStatus(iptablesOutput, "srv-1")

	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	r1 := rules[0]
	if r1.Port != "22" || r1.Protocol != "tcp" || r1.Interface != "eth0" || r1.Action != "allow" || r1.Direction != "ingress" {
		t.Errorf("r1 mismatch: %+v", r1)
	}

	r2 := rules[1]
	if r2.Port != "80" || r2.Protocol != "tcp" || r2.Source != "192.168.1.100" || r2.Action != "deny" {
		t.Errorf("r2 mismatch: %+v", r2)
	}

	r3 := rules[2]
	if r3.Port != "53" || r3.Protocol != "udp" || r3.Action != "allow" || r3.Direction != "egress" {
		t.Errorf("r3 mismatch: %+v", r3)
	}
}
