package config

import (
	"log"
	"os"
	"time"

	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	Server     ServerConfig
	Auth       AuthConfig
	Monitoring MonitoringConfig
	Docker     DockerConfig
	Database   DatabaseConfig
	GitHub     GitHubConfig
}

// GitHubConfig holds GitHub integration configuration
type GitHubConfig struct {
	ClientID     string
	ClientSecret string
	CallbackURL  string
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWTSecret     string
	TokenDuration time.Duration
	AdminUsername string
	AdminPassword string
}

// MonitoringConfig holds monitoring configuration
type MonitoringConfig struct {
	PollInterval    time.Duration
	EnableWebSocket bool
	StatsHistory    int
}

// DockerConfig holds Docker client configuration
type DockerConfig struct {
	Host    string
	Version string
}

// DatabaseConfig holds database service configuration
type DatabaseConfig struct {
	MongoHost       string
	PostgresHost    string
	MongoPort       int
	PostgresPort    int
	MongoLogPath    string
	PostgresLogPath string
}

// Load loads configuration from environment and config file
func Load() (*Config, error) {
	// Set defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.read_timeout", "15s")
	viper.SetDefault("server.write_timeout", "15s")
	viper.SetDefault("server.idle_timeout", "60s")

	viper.SetDefault("auth.token_duration", "24h")
	viper.SetDefault("auth.admin_username", "admin")
	viper.SetDefault("auth.admin_password", "changeme")

	viper.SetDefault("monitoring.poll_interval", "5s")
	viper.SetDefault("monitoring.enable_websocket", true)
	viper.SetDefault("monitoring.stats_history", 60)

	viper.SetDefault("docker.host", "unix:///var/run/docker.sock")
	viper.SetDefault("docker.version", "1.41")

	viper.SetDefault("database.mongo_host", "localhost")
	viper.SetDefault("database.postgres_host", "localhost")
	viper.SetDefault("database.mongo_port", 27017)
	viper.SetDefault("database.postgres_port", 5432)
	viper.SetDefault("database.mongo_log_path", "/var/log/mongodb/mongod.log")
	viper.SetDefault("database.postgres_log_path", "/var/log/postgresql/postgresql.log")

	// Read from config file if exists
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/gopulse/")

	// Allow environment variable overrides
	viper.SetEnvPrefix("GOPULSE")
	viper.AutomaticEnv()

	// Try to read config file (non-fatal if not found)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		log.Println("No config file found, using defaults and environment variables")
	}

	// JWT Secret is required
	jwtSecret := viper.GetString("auth.jwt_secret")
	if jwtSecret == "" {
		// Generate a random secret if not provided (for development)
		jwtSecret = os.Getenv("JWT_SECRET")
		if jwtSecret == "" {
			log.Println("WARNING: No JWT_SECRET provided, using insecure default. Set JWT_SECRET environment variable in production!")
			jwtSecret = "insecure-development-secret-change-in-production"
		}
	}

	config := &Config{
		Server: ServerConfig{
			Host:         viper.GetString("server.host"),
			Port:         viper.GetInt("server.port"),
			ReadTimeout:  viper.GetDuration("server.read_timeout"),
			WriteTimeout: viper.GetDuration("server.write_timeout"),
			IdleTimeout:  viper.GetDuration("server.idle_timeout"),
		},
		Auth: AuthConfig{
			JWTSecret:     jwtSecret,
			TokenDuration: viper.GetDuration("auth.token_duration"),
			AdminUsername: viper.GetString("auth.admin_username"),
			AdminPassword: viper.GetString("auth.admin_password"),
		},
		Monitoring: MonitoringConfig{
			PollInterval:    viper.GetDuration("monitoring.poll_interval"),
			EnableWebSocket: viper.GetBool("monitoring.enable_websocket"),
			StatsHistory:    viper.GetInt("monitoring.stats_history"),
		},
		Docker: DockerConfig{
			Host:    viper.GetString("docker.host"),
			Version: viper.GetString("docker.version"),
		},
		Database: DatabaseConfig{
			MongoHost:       viper.GetString("database.mongo_host"),
			PostgresHost:    viper.GetString("database.postgres_host"),
			MongoPort:       viper.GetInt("database.mongo_port"),
			PostgresPort:    viper.GetInt("database.postgres_port"),
			MongoLogPath:    viper.GetString("database.mongo_log_path"),
			PostgresLogPath: viper.GetString("database.postgres_log_path"),
		},
		GitHub: GitHubConfig{
			ClientID:     viper.GetString("github.client_id"),
			ClientSecret: viper.GetString("github.client_secret"),
			CallbackURL:  viper.GetString("github.callback_url"),
		},
	}

	return config, nil
}
