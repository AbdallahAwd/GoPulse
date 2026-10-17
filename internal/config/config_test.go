package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestJWTConfigurationFailsClosedAndHonorsEnvironment(t *testing.T) {
	for _, scenario := range []struct {
		name, secret string
		valid        bool
	}{
		{"missing", "", false}, {"short", "short", false}, {"old-default", "gopulse-super-secret-jwt-key-change-in-production", false}, {"configured", "isolated-configuration-secret-longer-than-32-characters", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			dir := t.TempDir()
			t.Chdir(dir)
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("auth:\n  jwt_secret: ''\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOPULSE_AUTH_JWT_SECRET", scenario.secret)
			t.Setenv("JWT_SECRET", "")
			cfg, err := Load()
			if (err == nil) != scenario.valid {
				t.Fatalf("configuration validation: %v", err)
			}
			if err == nil && cfg.Auth.JWTSecret != scenario.secret {
				t.Fatal("environment override ignored")
			}
		})
	}
}

func TestJWTConfigurationFromDotEnv(t *testing.T) {
	const fileSecret = "local-file-secret-longer-than-32-characters"
	const exportedSecret = "exported-secret-longer-than-32-characters"
	for _, scenario := range []struct {
		name, contents, exported, want string
		valid                          bool
	}{
		{"local secret", "GOPULSE_AUTH_JWT_SECRET=" + fileSecret, "", fileSecret, true},
		{"environment wins", "GOPULSE_AUTH_JWT_SECRET=" + fileSecret, exportedSecret, exportedSecret, true},
		{"legacy secret", "JWT_SECRET=" + fileSecret, "", fileSecret, true},
		{"short secret", "GOPULSE_AUTH_JWT_SECRET=short", "", "", false},
		{"malformed file", "invalid dotenv syntax", "", "", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			t.Chdir(t.TempDir())
			for _, key := range []string{"GOPULSE_AUTH_JWT_SECRET", "JWT_SECRET"} {
				// Register cleanup before unsetting; .env only fills unset variables.
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.exported != "" {
				t.Setenv("GOPULSE_AUTH_JWT_SECRET", scenario.exported)
			}
			if err := os.WriteFile(".env", []byte(scenario.contents+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("config.yaml", []byte("auth:\n  jwt_secret: ''\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if (err == nil) != scenario.valid {
				t.Fatalf("configuration validation: %v", err)
			}
			if err == nil && cfg.Auth.JWTSecret != scenario.want {
				t.Fatal("JWT secret source precedence ignored")
			}
		})
	}
}

func TestSafeDefaultsAndInvalidOperationalConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"defaults", "", ""},
		{"invalid port", "GOPULSE_SERVER_PORT", "65536"},
		{"invalid session duration", "GOPULSE_AUTH_TOKEN_DURATION", "0s"},
		{"invalid polling interval", "GOPULSE_MONITORING_POLL_INTERVAL", "-1s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()
			t.Chdir(t.TempDir())
			t.Setenv("GOPULSE_AUTH_JWT_SECRET", "test-configuration-secret-longer-than-32-characters")
			t.Setenv("GOPULSE_SERVER_HOST", "")
			if tc.key != "" {
				t.Setenv(tc.key, tc.value)
			}
			cfg, err := Load()
			if tc.key != "" {
				if err == nil {
					t.Fatal("invalid operational configuration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Server.Host != "127.0.0.1" {
				t.Fatalf("unsafe default listener: %s", cfg.Server.Host)
			}
		})
	}
}
