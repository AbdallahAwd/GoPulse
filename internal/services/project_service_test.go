package services

import "testing"

func TestNormalizeLaravelStartCommandUsesProjectPortForUIPreset(t *testing.T) {
	got := normalizeLaravelStartCommand("php artisan serve --port=8000 --host=0.0.0.0", 3000)
	want := "php artisan serve --port=3000 --host=0.0.0.0"

	if got != want {
		t.Fatalf("normalizeLaravelStartCommand() = %q, want %q", got, want)
	}
}

func TestNormalizeLaravelStartCommandPreservesCustomCommand(t *testing.T) {
	got := normalizeLaravelStartCommand("php artisan octane:start --server=swoole --host=0.0.0.0 --port=9000", 3000)
	want := "php artisan octane:start --server=swoole --host=0.0.0.0 --port=9000"

	if got != want {
		t.Fatalf("normalizeLaravelStartCommand() = %q, want %q", got, want)
	}
}

func TestPM2ShellStartCommandQuotesArguments(t *testing.T) {
	got := pm2ShellStartCommand("amy's api", "php artisan serve --port=3000 --host=0.0.0.0")
	want := "pm2 start /bin/sh --name 'amy'\"'\"'s api' -- -c 'exec php artisan serve --port=3000 --host=0.0.0.0'"

	if got != want {
		t.Fatalf("pm2ShellStartCommand() = %q, want %q", got, want)
	}
}
