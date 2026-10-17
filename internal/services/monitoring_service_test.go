package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

func newMonitoringTest(t *testing.T) (*MonitoringService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitoring.json")
	servers, err := store.NewServerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = servers.AddServer(models.Server{ID: "a", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	s, err := store.NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewMonitoringService(s, servers, 5*time.Second), path
}
func sampleStats(cpu float64) *models.SystemStats {
	return &models.SystemStats{CPU: models.CPUStats{Percent: cpu}, Memory: models.MemoryStats{UsedPercent: 40}, Disk: []models.DiskStats{{UsedPercent: 30}, {UsedPercent: 80}}}
}
func saveTestRule(t *testing.T, s *MonitoringService, metric string, duration int) models.AlertRule {
	t.Helper()
	rule, err := s.Save("a", "", AlertInput{Name: "Test", Metric: metric, Threshold: 85, DurationSeconds: duration, Enabled: true, WebhookURL: "https://hooks.example/events"})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}
func readTestRule(t *testing.T, s *MonitoringService, id string) models.AlertRule {
	t.Helper()
	r, e := s.Store.Rule("a", id)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func observeTest(t *testing.T, s *MonitoringService, stats *models.SystemStats, at time.Time) {
	t.Helper()
	if err := s.Observe("a", stats, at); err != nil {
		t.Fatal(err)
	}
}
func TestAlertDurationRecoveryMissingAndRestart(t *testing.T) {
	s, path := newMonitoringTest(t)
	rule := saveTestRule(t, s, "cpu", 10)
	now := time.Now().UTC().Add(time.Second)
	observeTest(t, s, sampleStats(90), now)
	if readTestRule(t, s, rule.ID).State != "pending" {
		t.Fatal("did not enter pending")
	}
	observeTest(t, s, sampleStats(90), now.Add(5*time.Second))
	// Construct a fresh service against persisted evaluator state.
	db, err := store.NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s = NewMonitoringService(db, s.Servers, 5*time.Second)
	observeTest(t, s, sampleStats(90), now.Add(10*time.Second))
	if r := readTestRule(t, s, rule.ID); r.State != "firing" || !r.IncidentOpen {
		t.Fatal(r)
	}
	observeTest(t, s, sampleStats(92), now.Add(15*time.Second))
	events, err := s.Store.Events("a")
	if err != nil || len(events) != 1 {
		t.Fatal("duplicate firing", events, err)
	}
	observeTest(t, s, nil, now.Add(20*time.Second))
	if r := readTestRule(t, s, rule.ID); r.State != "unknown" || !r.IncidentOpen || r.LastValue != nil {
		t.Fatal("missing data marked recovered", r)
	}
	observeTest(t, s, sampleStats(10), now.Add(25*time.Second))
	events, err = s.Store.Events("a")
	if err != nil || len(events) != 2 || events[0].State != "recovered" {
		t.Fatal(events, err)
	}
	observeTest(t, s, sampleStats(90), now.Add(30*time.Second))
	observeTest(t, s, sampleStats(90), now.Add(2*time.Minute))
	if r := readTestRule(t, s, rule.ID); r.State != "pending" || !r.BreachSince.Equal(now.Add(2*time.Minute)) {
		t.Fatal("gap counted toward duration", r)
	}
	observeTest(t, s, sampleStats(85), now.Add(2*time.Minute+5*time.Second))
	if readTestRule(t, s, rule.ID).State != "healthy" {
		t.Fatal("threshold equality must be healthy")
	}
}
func TestAvailabilityAndWatchdogDuration(t *testing.T) {
	s, _ := newMonitoringTest(t)
	rule := saveTestRule(t, s, "availability", 10)
	now := time.Now().UTC().Truncate(time.Minute).Add(time.Second)
	observeTest(t, s, sampleStats(10), now)
	s.markStale(now.Add(31 * time.Second))
	if r := readTestRule(t, s, rule.ID); r.State != "pending" {
		t.Fatal(r)
	}
	s.markStale(now.Add(36 * time.Second))
	s.markStale(now.Add(41 * time.Second))
	if r := readTestRule(t, s, rule.ID); r.State != "firing" {
		t.Fatal("watchdog continually reset duration", r)
	}
	observeTest(t, s, sampleStats(10), now.Add(45*time.Second))
	if r := readTestRule(t, s, rule.ID); r.State != "healthy" {
		t.Fatal(r)
	}
	history, err := s.Store.History("a", now.Add(time.Minute), time.Hour)
	if err != nil || len(history) != 1 {
		t.Fatal("watchdog fabricated metric samples", history, err)
	}
}
func TestSilenceExpiresWithoutLosingIncident(t *testing.T) {
	s, _ := newMonitoringTest(t)
	rule := saveTestRule(t, s, "cpu", 0)
	now := time.Now().UTC().Add(time.Second)
	if _, err := s.Silence("a", rule.ID, 60, now); err != nil {
		t.Fatal(err)
	}
	observeTest(t, s, sampleStats(90), now.Add(time.Second))
	events, _ := s.Store.Events("a")
	if len(events) != 1 || events[0].DeliveryStatus != "silenced" {
		t.Fatal(events)
	}
	observeTest(t, s, sampleStats(90), now.Add(10*time.Second))
	events, _ = s.Store.Events("a")
	if len(events) != 1 {
		t.Fatal("duplicate silenced event")
	}
	observeTest(t, s, sampleStats(90), now.Add(61*time.Second))
	observeTest(t, s, sampleStats(90), now.Add(62*time.Second))
	events, _ = s.Store.Events("a")
	if len(events) != 2 || events[0].DeliveryStatus != "pending" {
		t.Fatal("no firing after silence expired", events)
	}
	rule, err := s.Save("a", rule.ID, AlertInput{Name: "Paused", Metric: "cpu", Threshold: 85, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	observeTest(t, s, sampleStats(99), now.Add(63*time.Second))
	if readTestRule(t, s, rule.ID).State != "disabled" {
		t.Fatal("disabled rule evaluated")
	}
}
func TestConcurrentObservationsDeduplicateFiring(t *testing.T) {
	s, _ := newMonitoringTest(t)
	saveTestRule(t, s, "cpu", 0)
	now := time.Now().UTC().Add(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Observe("a", sampleStats(99), now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	events, err := s.Store.Events("a")
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
}

type monitoringTransport func(*http.Request) (*http.Response, error)

func (f monitoringTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSignedWebhookRetryAndRecovery(t *testing.T) {
	s, path := newMonitoringTest(t)
	rule := saveTestRule(t, s, "cpu", 0)
	now := time.Now().UTC().Add(time.Second)
	observeTest(t, s, sampleStats(99), now)
	var firstID, firstPayload string
	attempt := 0
	transport := monitoringTransport(func(r *http.Request) (*http.Response, error) {
		attempt++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(sha256.New, []byte(rule.WebhookSecret))
		mac.Write([]byte(r.Header.Get("X-GoPulse-Timestamp") + "."))
		mac.Write(body)
		if r.Header.Get("X-GoPulse-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("bad signature or content type")
		}
		if attempt == 1 {
			firstID = r.Header.Get("X-GoPulse-Event-ID")
			firstPayload = string(body)
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("untrusted response"))}, nil
		}
		if firstID != r.Header.Get("X-GoPulse-Event-ID") || firstPayload != string(body) {
			t.Fatal("retry changed immutable event")
		}
		return &http.Response{StatusCode: 204, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	s.client = &http.Client{Transport: transport}
	sent, err := s.DeliverNext(context.Background(), now)
	if err != nil || !sent {
		t.Fatal(sent, err)
	}
	events, _ := s.Store.Events("a")
	if events[0].DeliveryStatus != "retry" || events[0].Attempts != 1 {
		t.Fatal(events)
	}
	db, err := store.NewMonitoringStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s = NewMonitoringService(db, s.Servers, 5*time.Second)
	s.client = &http.Client{Transport: transport}
	sent, err = s.DeliverNext(context.Background(), now.Add(4*time.Second))
	if err != nil || sent {
		t.Fatal("retry ignored backoff", sent, err)
	}
	sent, err = s.DeliverNext(context.Background(), now.Add(5*time.Second))
	if err != nil || !sent {
		t.Fatal(sent, err)
	}
	events, _ = s.Store.Events("a")
	if events[0].DeliveryStatus != "delivered" || events[0].Attempts != 2 {
		t.Fatal(events)
	}
	observeTest(t, s, sampleStats(10), now.Add(10*time.Second))
	events, _ = s.Store.Events("a")
	if events[0].State != "recovered" || events[0].DeliveryStatus != "pending" {
		t.Fatal(events)
	}
}
func TestWebhookFailurePolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		network bool
		want    string
		retry   string
	}{{"bad request", 400, false, "failed", ""}, {"redirect", 302, false, "failed", ""}, {"timeout", 408, false, "retry", ""}, {"rate limited", 429, false, "retry", "120"}, {"server", 500, false, "retry", ""}, {"network", 0, true, "retry", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newMonitoringTest(t)
			rule := saveTestRule(t, s, "cpu", 0)
			now := time.Now().UTC().Add(time.Second)
			if err := s.Store.QueueTest("a", rule.ID, now); err != nil {
				t.Fatal(err)
			}
			s.client = &http.Client{Transport: monitoringTransport(func(*http.Request) (*http.Response, error) {
				if tc.network {
					return nil, errors.New("sensitive URL or secret must not leak")
				}
				return &http.Response{StatusCode: tc.code, Header: http.Header{"Retry-After": []string{tc.retry}}, Body: io.NopCloser(strings.NewReader("secret body"))}, nil
			})}
			if _, err := s.DeliverNext(context.Background(), now); err != nil {
				t.Fatal(err)
			}
			events, _ := s.Store.Events("a")
			if events[0].DeliveryStatus != tc.want || strings.Contains(events[0].LastError, "secret") {
				t.Fatal(events)
			}
			if tc.retry != "" {
				if sent, err := s.DeliverNext(context.Background(), now.Add(time.Minute)); err != nil || sent {
					t.Fatal("Retry-After ignored", sent, err)
				}
			}
		})
	}
}
func TestWebhookMaxAttempts(t *testing.T) {
	s, _ := newMonitoringTest(t)
	saveTestRule(t, s, "cpu", 0)
	now := time.Now().UTC().Add(time.Second)
	observeTest(t, s, sampleStats(99), now)
	s.client = &http.Client{Transport: monitoringTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	for i := 0; i < 8; i++ {
		if sent, err := s.DeliverNext(context.Background(), now.Add(time.Duration(i)*time.Hour)); err != nil || !sent {
			t.Fatal(sent, err)
		}
	}
	events, _ := s.Store.Events("a")
	if events[0].DeliveryStatus != "failed" || events[0].Attempts != 8 {
		t.Fatal(events)
	}
}
func TestWebhookDestinationProtectionAndValidation(t *testing.T) {
	for _, raw := range []string{"http://hooks.example", "https://user:password@hooks.example", "https://127.0.0.1/x", "https://[::1]/x", "https://169.254.169.254/metadata", "https://10.0.0.1/x", "https://hooks.example/x#fragment"} {
		if validateWebhook(raw) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := validateWebhook("https://hooks.example/events?token=example"); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "::ffff:127.0.0.1", "fd00::1", "fe80::1", "2001:db8::1", "198.18.0.1", "0.0.0.0"} {
		if publicWebhookIP(netip.MustParseAddr(ip)) {
			t.Errorf("accepted private/reserved %s", ip)
		}
	}
	client := newWebhookClient()
	_, err := client.Get("https://localhost/hook")
	if err == nil {
		t.Fatal("DNS-resolved loopback was not blocked")
	}
	s, _ := newMonitoringTest(t)
	for _, input := range []AlertInput{{Name: "", Metric: "cpu"}, {Name: "x", Metric: "invalid"}, {Name: "x", Metric: "cpu", Threshold: 101}, {Name: "x", Metric: "cpu", DurationSeconds: -1}, {Name: "x", Metric: "cpu", Threshold: math.NaN()}} {
		if _, err := s.Save("a", "", input); err == nil {
			t.Fatal("accepted invalid rule", input)
		}
	}
}
func TestSnapshotRejectsInvalidDataAndUsesHighestDisk(t *testing.T) {
	now := time.Now()
	if sample := Snapshot(sampleStats(10), now); sample.DiskPercent != 80 || sample.Unavailable {
		t.Fatal(sample)
	}
	if !Snapshot(sampleStats(math.NaN()), now).Unavailable || !Snapshot(nil, now).Unavailable || !Snapshot(&models.SystemStats{}, now).Unavailable {
		t.Fatal("missing/invalid data became zero")
	}
}
func TestRemoteCPUParserNeverSubstitutesZeroOnFailure(t *testing.T) {
	got, err := parseRemoteCPUPercent("%Cpu(s): 1.0 us, 2.0 sy, 0.0 ni, 97.0 id, 0.0 wa")
	if err != nil || got != 3 {
		t.Fatal(got, err)
	}
	for _, raw := range []string{"", "invalid", "%Cpu(s): 0.0 us", "NaN id", "101 id"} {
		if _, err := parseRemoteCPUPercent(raw); err == nil {
			t.Fatal("accepted bad CPU reading", raw)
		}
	}
}

func TestRecoveryCannotOvertakeRetryingFiringEvent(t *testing.T) {
	s, _ := newMonitoringTest(t)
	saveTestRule(t, s, "cpu", 0)
	now := time.Now().UTC().Add(time.Second)
	observeTest(t, s, sampleStats(99), now)
	s.client = &http.Client{Transport: monitoringTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if sent, err := s.DeliverNext(context.Background(), now); err != nil || !sent {
		t.Fatal(sent, err)
	}
	observeTest(t, s, sampleStats(10), now.Add(time.Second))
	if sent, err := s.DeliverNext(context.Background(), now.Add(2*time.Second)); err != nil || sent {
		t.Fatal("recovery overtook firing event", sent, err)
	}
}

func TestWebhookRealTLSDelivery(t *testing.T) {
	s, _ := newMonitoringTest(t)
	rule := saveTestRule(t, s, "cpu", 0)
	now := time.Now().UTC().Add(time.Second)
	observeTest(t, s, sampleStats(99), now)
	var received atomic.Bool
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mac := hmac.New(sha256.New, []byte(rule.WebhookSecret))
		mac.Write([]byte(r.Header.Get("X-GoPulse-Timestamp") + "."))
		mac.Write(body)
		if r.Method != "POST" || r.URL.Path != "/events" || r.Header.Get("X-GoPulse-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Error("invalid signed TLS request")
		}
		received.Store(true)
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	localClient := receiver.Client()
	s.client = &http.Client{Transport: monitoringTransport(func(r *http.Request) (*http.Response, error) {
		target, err := url.Parse(receiver.URL + "/events")
		if err != nil {
			return nil, err
		}
		r.URL = target
		return localClient.Transport.RoundTrip(r)
	})}
	if sent, err := s.DeliverNext(context.Background(), now); err != nil || !sent || !received.Load() {
		t.Fatal(sent, received.Load(), err)
	}
	events, err := s.Store.Events("a")
	if err != nil || events[0].DeliveryStatus != "delivered" {
		t.Fatal(events, err)
	}
}

func TestDiskCapacityIgnoresReadOnlyImages(t *testing.T) {
	for _, tc := range []struct {
		fs       string
		ro, want bool
	}{{"ext4", false, true}, {"xfs", false, true}, {"tmpfs", false, true}, {"squashfs", false, false}, {"iso9660", false, false}, {"udf", false, false}, {"apfs", true, false}, {"ext4", true, false}, {"devfs", false, false}, {"proc", false, false}, {"cgroup2", false, false}} {
		if got := monitorDiskPartition(tc.fs, tc.ro); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestMonitoringWorkerStopsOnCancellation(t *testing.T) {
	s, _ := newMonitoringTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Start(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitoring worker did not stop")
	}
}

func TestSlowWebhookCannotBlockStaleRuleEvaluation(t *testing.T) {
	s, _ := newMonitoringTest(t)
	rule := saveTestRule(t, s, "availability", 0)
	now := time.Now().UTC()
	observeTest(t, s, sampleStats(10), now)
	s.MaxGap = 50 * time.Millisecond
	if err := s.Store.QueueTest("a", rule.ID, now); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	s.client = &http.Client{Transport: monitoringTransport(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Start(ctx); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("worker failed to stop")
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("notification did not start")
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if readTestRule(t, s, rule.ID).State == "firing" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("slow receiver blocked the stale-metrics watchdog")
}
