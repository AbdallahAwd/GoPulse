package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
)

type AlertValidationError struct{ Message string }

func (e *AlertValidationError) Error() string { return e.Message }
func invalidAlert(message string) error       { return &AlertValidationError{message} }

type AlertInput struct {
	Name            string  `json:"name"`
	Metric          string  `json:"metric"`
	Threshold       float64 `json:"threshold"`
	DurationSeconds int     `json:"duration_seconds"`
	Enabled         bool    `json:"enabled"`
	WebhookURL      string  `json:"webhook_url"`
}

type MonitoringService struct {
	Store   *store.MonitoringStore
	Servers *store.ServerStore
	MaxGap  time.Duration
	client  *http.Client
}

func NewMonitoringService(s *store.MonitoringStore, servers *store.ServerStore, interval time.Duration) *MonitoringService {
	gap := 3 * interval
	if gap < 30*time.Second {
		gap = 30 * time.Second
	}
	return &MonitoringService{Store: s, Servers: servers, MaxGap: gap, client: newWebhookClient()}
}
func validateWebhook(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return invalidAlert("Webhook must be an HTTPS URL without credentials or a fragment (maximum 2048 characters).")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicWebhookIP(ip) {
		return invalidAlert("Webhook destinations must use public Internet addresses.")
	}
	return nil
}
func (s *MonitoringService) Save(serverID, id string, input AlertInput) (models.AlertRule, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.WebhookURL = strings.TrimSpace(input.WebhookURL)
	if input.Name == "" || len(input.Name) > 100 {
		return models.AlertRule{}, invalidAlert("Rule name must contain 1–100 characters.")
	}
	switch input.Metric {
	case "cpu", "memory", "disk", "availability":
	default:
		return models.AlertRule{}, invalidAlert("Choose CPU, memory, disk or availability.")
	}
	if math.IsNaN(input.Threshold) || math.IsInf(input.Threshold, 0) || input.Threshold < 0 || input.Threshold > 100 {
		return models.AlertRule{}, invalidAlert("Threshold must be between 0 and 100 percent.")
	}
	if input.DurationSeconds < 0 || input.DurationSeconds > 86400 {
		return models.AlertRule{}, invalidAlert("Duration must be between 0 and 86400 seconds.")
	}
	if err := validateWebhook(input.WebhookURL); err != nil {
		return models.AlertRule{}, err
	}
	if input.Metric == "availability" {
		input.Threshold = 0
	}
	create := id == ""
	if create {
		id = uuid.NewString()
	}
	return s.Store.MutateRule(serverID, id, create, func(rule *models.AlertRule) error {
		key := rule.WebhookSecret
		if key == "" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			key = hex.EncodeToString(b)
		}
		silence := rule.SilencedUntil
		*rule = models.AlertRule{ID: id, ServerID: serverID, Name: input.Name, Metric: input.Metric, Threshold: input.Threshold, DurationSeconds: input.DurationSeconds, Enabled: input.Enabled, WebhookURL: input.WebhookURL, HasWebhook: input.WebhookURL != "", WebhookSecret: key, State: "unknown", SilencedUntil: silence, LastSampleAt: time.Now().UTC()}
		if !rule.Enabled {
			rule.State = "disabled"
		}
		return nil
	})
}
func (s *MonitoringService) Silence(serverID, id string, seconds int, now time.Time) (models.AlertRule, error) {
	if seconds < 0 || seconds > 7*86400 {
		return models.AlertRule{}, invalidAlert("Silence must be between 0 and 604800 seconds.")
	}
	return s.Store.MutateRule(serverID, id, false, func(rule *models.AlertRule) error {
		rule.SilencedUntil = time.Time{}
		if seconds > 0 {
			rule.SilencedUntil = now.Add(time.Duration(seconds) * time.Second)
		}
		return nil
	})
}

func Snapshot(stats *models.SystemStats, now time.Time) models.StatsSnapshot {
	sample := models.StatsSnapshot{Timestamp: now, Unavailable: stats == nil || len(stats.Disk) == 0}
	if sample.Unavailable {
		return sample
	}
	sample.CPUPercent = stats.CPU.Percent
	sample.MemPercent = stats.Memory.UsedPercent
	for _, disk := range stats.Disk {
		if math.IsNaN(disk.UsedPercent) || math.IsInf(disk.UsedPercent, 0) || disk.UsedPercent < 0 || disk.UsedPercent > 100 {
			return models.StatsSnapshot{Timestamp: now, Unavailable: true}
		}
		if disk.UsedPercent > sample.DiskPercent {
			sample.DiskPercent = disk.UsedPercent
		}
	}
	sample.NetRxBytes = stats.Network.BytesRecv
	sample.NetTxBytes = stats.Network.BytesSent
	for _, value := range []float64{sample.CPUPercent, sample.MemPercent, sample.DiskPercent} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
			return models.StatsSnapshot{Timestamp: now, Unavailable: true}
		}
	}
	return sample
}
func (s *MonitoringService) Observe(serverID string, stats *models.SystemStats, now time.Time) error {
	sample := Snapshot(stats, now)
	return s.Store.Observe(serverID, sample, true, func(rule *models.AlertRule) *models.AlertEvent {
		rule.LastSampleAt = now
		return evaluateAlert(rule, sample, s.MaxGap)
	})
}
func evaluateAlert(rule *models.AlertRule, sample models.StatsSnapshot, maxGap time.Duration) *models.AlertEvent {
	now := sample.Timestamp
	if !rule.LastObservedAt.IsZero() && !now.After(rule.LastObservedAt) {
		return nil
	}
	gap := rule.LastObservedAt.IsZero() || now.Sub(rule.LastObservedAt) > maxGap
	rule.LastObservedAt = now
	if !rule.Enabled {
		rule.State = "disabled"
		rule.BreachSince = time.Time{}
		rule.LastValue = nil
		return nil
	}
	if sample.Unavailable && rule.Metric != "availability" {
		rule.State = "unknown"
		rule.LastValue = nil
		rule.BreachSince = time.Time{}
		return nil
	}
	value := sample.CPUPercent
	switch rule.Metric {
	case "memory":
		value = sample.MemPercent
	case "disk":
		value = sample.DiskPercent
	case "availability":
		value = 100
		if sample.Unavailable {
			value = 0
		}
	}
	rule.LastValue = &value
	breached := value > rule.Threshold
	if rule.Metric == "availability" {
		breached = sample.Unavailable
	}
	if !breached {
		rule.State = "healthy"
		rule.BreachSince = time.Time{}
		if rule.IncidentOpen {
			rule.IncidentOpen = false
			rule.NotifiedFiring = false
			return &models.AlertEvent{State: "recovered", Value: &value, OccurredAt: now}
		}
		return nil
	}
	if gap || rule.BreachSince.IsZero() {
		rule.BreachSince = now
	}
	if !rule.IncidentOpen && now.Sub(rule.BreachSince) < time.Duration(rule.DurationSeconds)*time.Second {
		rule.State = "pending"
		return nil
	}
	rule.State = "firing"
	opened := !rule.IncidentOpen
	rule.IncidentOpen = true
	if opened || (!rule.NotifiedFiring && !rule.SilencedUntil.After(now)) {
		if !rule.SilencedUntil.After(now) {
			rule.NotifiedFiring = true
		}
		return &models.AlertEvent{State: "firing", Value: &value, OccurredAt: now}
	}
	return nil
}

// Start keeps evaluation/retention independent of slow webhook receivers.
func (s *MonitoringService) Start(ctx context.Context) {
	delivered := make(chan struct{})
	go func() { defer close(delivered); s.deliveryLoop(ctx) }()
	defer func() { <-delivered }()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	if err := s.Store.Prune(time.Now()); err != nil {
		log.Printf("Monitoring retention: %v", err)
	}
	lastPrune := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.markStale(now)
			if now.Sub(lastPrune) >= time.Hour {
				if err := s.Store.Prune(now); err != nil {
					log.Printf("Monitoring retention: %v", err)
				}
				lastPrune = now
			}
		}
	}
}
func (s *MonitoringService) deliveryLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer s.client.CloseIdleConnections()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 5 && ctx.Err() == nil; i++ {
				sent, err := s.DeliverNext(ctx, time.Now().UTC())
				if err != nil {
					log.Printf("Notification outbox: %v", err)
					break
				}
				if !sent {
					break
				}
			}
		}
	}
}

func (s *MonitoringService) markStale(now time.Time) {
	for _, server := range s.Servers.ListServers() {
		err := s.Store.Observe(server.ID, models.StatsSnapshot{Timestamp: now, Unavailable: true}, false, func(rule *models.AlertRule) *models.AlertEvent {
			if !rule.Enabled || now.Sub(rule.LastSampleAt) <= s.MaxGap {
				return nil
			}
			return evaluateAlert(rule, models.StatsSnapshot{Timestamp: now, Unavailable: true}, s.MaxGap)
		})
		if err != nil {
			log.Printf("Stale metrics evaluation: %v", err)
		}
	}
}

func publicWebhookIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/32", "2001:db8::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(p).Contains(ip) {
			return false
		}
	}
	return true
}
func newWebhookClient() *http.Client {
	transport := &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid destination")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("destination lookup failed")
		}
		for _, ip := range ips {
			if !publicWebhookIP(ip) {
				return nil, fmt.Errorf("private or reserved destination blocked")
			}
		}
		var conn net.Conn
		for _, ip := range ips {
			conn, err = (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("destination connection failed")
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// DeliverNext provides at-least-once delivery. Consumers deduplicate the stable
// X-GoPulse-Event-ID; the signature covers timestamp + '.' + exact JSON bytes.
func (s *MonitoringService) DeliverNext(ctx context.Context, now time.Time) (bool, error) {
	delivery, err := s.Store.ClaimDelivery(now)
	if err != nil || delivery == nil {
		return false, err
	}
	status, message := "retry", "Webhook connection failed"
	retryDelay := time.Duration(5*(1<<min(delivery.Event.Attempts-1, 9))) * time.Second
	if err = validateWebhook(delivery.URL); err != nil {
		status, message = "failed", "Webhook destination is invalid"
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, strings.NewReader(string(delivery.Payload)))
		if reqErr == nil {
			timestamp := strconv.FormatInt(now.Unix(), 10)
			mac := hmac.New(sha256.New, []byte(delivery.Secret))
			mac.Write([]byte(timestamp + "."))
			mac.Write(delivery.Payload)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "GoPulse-Webhooks/1")
			req.Header.Set("X-GoPulse-Event-ID", delivery.Event.ID)
			req.Header.Set("X-GoPulse-Timestamp", timestamp)
			req.Header.Set("X-GoPulse-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			response, sendErr := s.client.Do(req)
			if sendErr == nil {
				io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
				response.Body.Close()
				message = fmt.Sprintf("Webhook returned HTTP %d", response.StatusCode)
				if response.StatusCode >= 200 && response.StatusCode < 300 {
					status, message = "delivered", ""
				} else if response.StatusCode != 408 && response.StatusCode != 429 && response.StatusCode < 500 {
					status = "failed"
				}
				if response.StatusCode == 429 {
					if sec, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && sec > 0 {
						retryDelay = time.Duration(min(sec, 3600)) * time.Second
					}
				}
			}
		} else {
			status, message = "failed", "Webhook request is invalid"
		}
	}
	if delivery.Event.Attempts >= 8 && status == "retry" {
		status = "failed"
	}
	return true, s.Store.CompleteDelivery(delivery.Event.ID, status, message, now.Add(retryDelay))
}
