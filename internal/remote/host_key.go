package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHHostKey is an observation, not a trust decision. No login credentials are
// used to fetch it; an operator must independently verify the fingerprint.
type SSHHostKey struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
}

func NormalizeSSHEndpoint(host string, port int) (string, int, error) {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") && net.ParseIP(host[1:len(host)-1]) != nil {
		host = host[1 : len(host)-1]
	}
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "\x00\r\n\t /@\\[]") || (strings.Contains(host, ":") && net.ParseIP(host) == nil) {
		return "", 0, fmt.Errorf("a hostname or IP address without a port is required")
	}
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("SSH port must be between 1 and 65535")
	}
	return host, port, nil
}

// ParseHostKey accepts one bare OpenSSH public key, never authorized_keys
// options or multiple entries. Canonical storage excludes comments.
func ParseHostKey(raw string) (ssh.PublicKey, error) {
	if len(raw) > 16384 {
		return nil, fmt.Errorf("SSH host key is too large")
	}
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "\r\n") {
		return nil, fmt.Errorf("a single SSH public host key is required")
	}
	key, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(raw))
	if err != nil || len(options) != 0 || strings.TrimSpace(string(rest)) != "" {
		return nil, fmt.Errorf("a single SSH public host key is required")
	}
	return key, nil
}

var errHostKeyObserved = errors.New("SSH host key observed; stop before authentication")

func ProbeSSHHostKey(parent context.Context, host string, port int) (SSHHostKey, error) {
	host, port, err := NormalizeSSHEndpoint(host, port)
	if err != nil {
		return SSHHostKey{}, err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	address := net.JoinHostPort(host, fmt.Sprint(port))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return SSHHostKey{}, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return SSHHostKey{}, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	var observed ssh.PublicKey
	_, _, _, err = ssh.NewClientConn(conn, address, &ssh.ClientConfig{
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			observed = key
			return errHostKeyObserved
		},
	})
	if !errors.Is(err, errHostKeyObserved) || observed == nil {
		if err == nil {
			err = fmt.Errorf("server did not provide an SSH host key")
		}
		return SSHHostKey{}, err
	}
	return SSHHostKey{Host: host, Port: port, Algorithm: observed.Type(), Fingerprint: ssh.FingerprintSHA256(observed), PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(observed)))}, nil
}

// Pin algorithm negotiation to the approved key type, including modern RSA
// signature names. A different host key must not silently replace this pin.
func pinnedHostKeyAlgorithms(key ssh.PublicKey) []string {
	switch key.Type() {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	case ssh.CertAlgoRSAv01:
		return []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01}
	default:
		return []string{key.Type()}
	}
}
