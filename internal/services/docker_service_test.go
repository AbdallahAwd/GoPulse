package services

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDockerSDKOperationsAgainstLegacyAPI(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("API-Version", "1.41")
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/v1.41")
		switch {
		case path == "/_ping":
			if r.Method != "HEAD" {
				io.WriteString(w, "OK")
			}
		case path == "/containers/json":
			io.WriteString(w, `[{"Id":"short-id","Names":["/sample"],"Image":"alpine:latest","State":"running","Status":"Up","Ports":[{"PrivatePort":80,"PublicPort":8080,"Type":"tcp"}]}]`)
		case path == "/images/json":
			io.WriteString(w, `[{"Id":"sha256:image-id","RepoTags":["alpine:latest"],"Size":123,"Created":123}]`)
		case path == "/volumes" && r.Method == "GET":
			io.WriteString(w, `{"Volumes":[{"Name":"data","Driver":"local","Mountpoint":"/data","CreatedAt":"2026-01-01T00:00:00Z"}]}`)
		case path == "/networks" && r.Method == "GET":
			io.WriteString(w, `[{"Id":"network-id","Name":"bridge","Driver":"bridge","Scope":"local"}]`)
		case path == "/containers/short-id/logs":
			payload := []byte("2026-01-01T00:00:00.000000000Z hello\n")
			frame := make([]byte, 8)
			frame[0] = 1
			binary.BigEndian.PutUint32(frame[4:], uint32(len(payload)))
			w.Write(append(frame, payload...))
		case path == "/containers/short-id/json":
			io.WriteString(w, `{"Id":"short-id","Config":{"Tty":false}}`)
		case path == "/images/create":
			io.WriteString(w, "{\"status\":\"complete\"}\n")
		case path == "/images/image-id" && r.Method == "DELETE":
			io.WriteString(w, `[{"Deleted":"sha256:image-id"}]`)
		case r.Method == "POST" || r.Method == "DELETE":
			if (strings.HasSuffix(path, "/stop") || strings.HasSuffix(path, "/restart")) && r.URL.Query().Get("t") != "10" {
				t.Error("stop timeout was not preserved")
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker SDK request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()
	service, err := NewDockerService(strings.Replace(daemon.URL, "http://", "tcp://", 1), "1.41")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	containers, err := service.ListContainers(ctx)
	if err != nil || len(containers) != 1 || containers[0].ID != "short-id" || containers[0].Name != "sample" || containers[0].Ports[0] != "8080:80/tcp" {
		t.Fatalf("container mapping: %+v, %v", containers, err)
	}
	images, err := service.ListImages(ctx)
	if err != nil || len(images) != 1 || images[0].ID != "image-id" || images[0].Size != 123 {
		t.Fatalf("image mapping: %+v, %v", images, err)
	}
	volumes, err := service.ListVolumes(ctx)
	if err != nil || len(volumes) != 1 || volumes[0].Name != "data" {
		t.Fatalf("volume mapping: %+v, %v", volumes, err)
	}
	networks, err := service.ListNetworks(ctx)
	if err != nil || len(networks) != 1 || networks[0].Name != "bridge" {
		t.Fatalf("network mapping: %+v, %v", networks, err)
	}
	logs, err := service.GetContainerLogs(ctx, "short-id")
	if err != nil || len(logs) != 1 || logs[0].Message != "hello" {
		t.Fatalf("log demultiplexing: %+v, %v", logs, err)
	}
	for _, call := range []func(context.Context, string) error{service.StartContainer, service.StopContainer, service.RestartContainer, service.RemoveContainer} {
		if err := call(ctx, "short-id"); err != nil {
			t.Fatal(err)
		}
	}
	for _, call := range []func(context.Context, string) error{service.RemoveImage, service.RemoveVolume, service.RemoveNetwork} {
		if err := call(ctx, "image-id"); err != nil {
			t.Fatal(err)
		}
	}
	stream, err := service.PullImage(ctx, "alpine:latest")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || !strings.Contains(string(body), "complete") {
		t.Fatalf("image pull stream: %s, %v", body, err)
	}
}
