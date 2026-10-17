package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

func TestFunctionStoreRetentionPersistenceAndSecretBoundary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	servers, e := NewServerStore(file)
	if e != nil {
		t.Fatal(e)
	}
	if e = servers.AddServer(models.Server{ID: "a"}); e != nil {
		t.Fatal(e)
	}
	st, e := NewFunctionStore(file)
	if e != nil {
		t.Fatal(e)
	}
	f := models.Function{ID: uuid.NewString(), ServerID: "a", Draft: models.FunctionSpec{Name: "Hello"}}
	if e = st.Save(f, true); e != nil {
		t.Fatal(e)
	}
	if e = st.SetTokenHash("a", f.ID, "private-hash"); e != nil {
		t.Fatal(e)
	}
	for i := range 25 {
		v := models.FunctionVersion{ID: uuid.NewString(), FunctionID: f.ID, Revision: i}
		f.Deployed = &v
		if e = st.Activate(f, v); e != nil {
			t.Fatal(e)
		}
	}
	for i := range 110 {
		r := models.FunctionRun{ID: uuid.NewString(), FunctionID: f.ID, ServerID: "a", Kind: "invoke", Status: "skipped", StartedAt: time.Now().Add(time.Duration(i) * time.Second)}
		if e = st.RecordSkipped(r); e != nil {
			t.Fatal(e)
		}
	}
	old := models.FunctionRun{ID: uuid.NewString(), FunctionID: f.ID, ServerID: "a", Kind: "invoke", Status: "failed", NeedsCleanup: true, StartedAt: time.Now().Add(-31 * 24 * time.Hour)}
	if e = st.RecordSkipped(old); e != nil {
		t.Fatal(e)
	}
	if e = st.UpdateRun(old); e != nil {
		t.Fatal(e)
	}
	if e = st.Prune(time.Now()); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = st.db.QueryRow(`SELECT COUNT(*) FROM function_runs WHERE function_id=?`, f.ID).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 100 {
		t.Fatal("history bound", count)
	}
	if _, e = st.Run(f.ID, old.ID); e != nil {
		t.Fatal("cleanup record pruned", e)
	}
	if e = st.db.QueryRow(`SELECT COUNT(*) FROM function_versions WHERE function_id=?`, f.ID).Scan(&count); e != nil || count != 20 {
		t.Fatal("version bound", count, e)
	}
	reopened, e := NewFunctionStore(file)
	if e != nil {
		t.Fatal(e)
	}
	got, e := reopened.Get("a", f.ID)
	if e != nil || got.Deployed.ID != f.Deployed.ID {
		t.Fatal(got, e)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "private-hash") {
		t.Fatal("token hash exposed")
	}
	if e = st.Delete("a", f.ID); e != ErrFunctionBusy {
		t.Fatal("deleted pending cleanup", e)
	}
	old.NeedsCleanup = false
	st.UpdateRun(old)
	if e = st.Delete("a", f.ID); e != nil {
		t.Fatal(e)
	}
}
