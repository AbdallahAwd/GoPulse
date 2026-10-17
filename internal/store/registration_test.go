package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func TestConcurrentRegistrationCreatesOnlyOneAdministrator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	first, err := NewUserStore(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewUserStore(path)
	if err != nil {
		t.Fatal(err)
	}
	stores := []*UserStore{first, second}
	const requests = 24
	start := make(chan struct{})
	results := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results <- stores[i%2].RegisterFirstUser(models.User{
				Username: fmt.Sprintf("candidate-%d", i), Password: "test-hash",
			})
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, ErrRegistrationClosed) {
			t.Fatalf("unexpected registration error: %v", err)
		}
	}
	users := first.ListUsers()
	if created != 1 || len(users) != 1 {
		t.Fatalf("created %d accounts; stored %d, want exactly one", created, len(users))
	}
	if users[0].Role != "admin" || len(users[0].Permissions) != 1 || users[0].Permissions[0] != "*" {
		t.Fatal("first account did not receive administrator permissions")
	}
}
