package store

import (
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"path/filepath"
	"testing"
)

func TestStaleHealthProbeCannotReenableDisabledBackend(t *testing.T) {
	st, err := NewLoadBalancerStore(filepath.Join(t.TempDir(), "lbs.json"))
	if err != nil {
		t.Fatal(err)
	}
	lb := models.LoadBalancer{ID: "lb", ServerID: "srv", Backends: []models.LoadBalancerBackend{{ID: "backend", Status: "disabled"}}}
	if err := st.AddLoadBalancer(lb); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateBackendStatus("backend", "healthy"); err != nil {
		t.Fatal(err)
	}
	saved, err := st.GetLoadBalancer("lb")
	if err != nil || saved.Backends[0].Status != "disabled" {
		t.Fatalf("disabled state lost: %+v %v", saved, err)
	}
}
