# GoPulse - VPS Management Dashboard

A production-ready monolithic VPS management dashboard built entirely in Go. Single binary deployment with embedded frontend assets.

## Features

- 📊 **Real-time System Monitoring**: CPU, RAM, Disk, Network stats with WebSocket updates
- 🔄 **Process Management**: List, search, and kill processes; manage systemd services
- 🐳 **Docker Integration**: Manage containers, view stats, stream logs
- 🗄️ **Database Monitoring**: Health checks and service management for MongoDB & PostgreSQL
- 🔐 **Secure Authentication**: JWT + Basic Auth with configurable credentials
- ⚡ **High Performance**: Concurrent monitoring with goroutines and in-memory caching
- 📦 **Single Binary**: All assets embedded - no external dependencies

## Tech Stack

- **Backend**: Go 1.21+
- **Router**: Chi
- **Frontend**: HTML5 + Tailwind CSS + Vanilla JavaScript
- **WebSockets**: Gorilla WebSocket
- **System Metrics**: gopsutil
- **Docker SDK**: Official Docker client for Go

## Quick Start

### Prerequisites

- Go 1.21 or higher
- Docker (optional, for container management)
- MongoDB (optional, for database monitoring)
- PostgreSQL (optional, for database monitoring)

### Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd GoPulse
```

2. Install dependencies:
```bash
make deps
```

3. Build the application:
```bash
make build
```

4. Run the application:
```bash
./bin/gopulse
```

The dashboard will be available at `http://localhost:8080`

**Default credentials**: `admin` / `changeme`

### Configuration

Create a `config.yaml` file in the application directory:

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: "15s"
  write_timeout: "15s"
  idle_timeout: "60s"

auth:
  jwt_secret: "your-super-secret-key-change-this"
  admin_username: "admin"
  admin_password: "changeme"
  token_duration: "24h"

monitoring:
  poll_interval: "5s"
  enable_websocket: true
  stats_history: 60

docker:
  host: "unix:///var/run/docker.sock"
  version: "1.41"

database:
  mongo_host: "localhost"
  mongo_port: 27017
  postgres_host: "localhost"
  postgres_port: 5432
  mongo_log_path: "/var/log/mongodb/mongod.log"
  postgres_log_path: "/var/log/postgresql/postgresql.log"
```

### Environment Variables

You can also use environment variables (prefix with `GOPULSE_`):

```bash
export GOPULSE_SERVER_PORT=8080
export GOPULSE_AUTH_JWT_SECRET="your-secret-key"
export GOPULSE_AUTH_ADMIN_USERNAME="admin"
export GOPULSE_AUTH_ADMIN_PASSWORD="secure-password"
```

## Usage

### API Endpoints

#### Authentication
- `POST /auth/login` - Get JWT token

#### System Stats
- `GET /api/stats` - Get current system statistics
- `GET /api/stats/ws` - WebSocket endpoint for real-time updates

#### Process Management
- `GET /api/processes` - List all processes
- `GET /api/processes?q={query}` - Search processes
- `GET /api/processes/{pid}` - Get process details
- `DELETE /api/processes/{pid}` - Kill a process
- `GET /api/services/{name}/status` - Get service status
- `POST /api/services/{name}/restart` - Restart a service

#### Docker Management
- `GET /api/containers` - List containers
- `GET /api/containers/{id}/stats` - Get container stats
- `GET /api/containers/{id}/logs` - Get container logs
- `POST /api/containers/{id}/start` - Start container
- `POST /api/containers/{id}/stop` - Stop container
- `POST /api/containers/{id}/restart` - Restart container

#### Database Management
- `GET /api/databases/mongodb/health` - Check MongoDB health
- `GET /api/databases/postgresql/health` - Check PostgreSQL health
- `POST /api/databases/mongodb/restart` - Restart MongoDB
- `POST /api/databases/postgresql/restart` - Restart PostgreSQL
- `GET /api/databases/{service}/logs` - Get database logs

## Development

### Run in development mode

```bash
make run
```

### Run tests

```bash
make test
```

### Build for production

```bash
make build-prod
```

### Docker deployment

```bash
# Build Docker image
make docker

# Run container
make docker-run
```

## Architecture

```
GoPulse/
├── cmd/server/          # Application entry point
├── internal/
│   ├── config/          # Configuration management
│   ├── middleware/      # HTTP middleware (auth, logging, rate limiting)
│   ├── handlers/        # HTTP request handlers
│   ├── services/        # Business logic layer
│   ├── monitor/         # Background stats collector
│   └── models/          # Data structures
├── web/
│   ├── templates/       # HTML templates
│   └── static/          # CSS and JavaScript
└── Makefile            # Build automation
```

### Key Design Patterns

1. **Embedded Assets**: Frontend bundled into binary using `//go:embed`
2. **Background Workers**: Goroutines for concurrent system monitoring
3. **Thread-safe Caching**: RWMutex for stats cache access
4. **Middleware Chain**: Composable middleware for cross-cutting concerns
5. **Clean Architecture**: Separation of concerns (handlers → services → data)

## Performance

- **Memory**: ~50MB base + caching overhead
- **CPU**: <1% idle, <5% during active monitoring
- **WebSocket**: Supports 1000+ concurrent connections
- **Stats Collection**: Non-blocking, 5-second intervals

## Security

- JWT-based authentication with configurable expiry
- Rate limiting (100 requests/minute per IP)
- CORS protection
- Request logging for audit trails
- Basic auth fallback for initial setup

## Troubleshooting

### Docker not available
If Docker is not installed or the daemon is not running, Docker features will be disabled. The application will continue to work with other features.

### Permission denied errors
Some operations require elevated privileges:
- Process killing may need appropriate permissions
- Service management requires systemd/root access
- Docker socket access may need user to be in `docker` group

### Database logs not accessible
Ensure the application has read permissions for log files:
```bash
sudo chmod +r /var/log/mongodb/mongod.log
sudo chmod +r /var/log/postgresql/postgresql.log
```

## License

MIT License - see LICENSE file for details

## Contributing

Contributions are welcome! Please submit pull requests with:
- Clear description of changes
- Tests for new functionality
- Updated documentation

## Support

For issues and questions, please open an issue on GitHub.
