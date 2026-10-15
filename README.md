# GoPulse - VPS Management Dashboard

A monolithic VPS management dashboard built in Go, with a single binary and embedded frontend assets. See the verification report for deployment prerequisites and tested limitations.

## Features

- 📊 **Real-time System Monitoring**: CPU, RAM, Disk, Network stats with WebSocket updates
- 🔄 **Process Management**: List, search, and kill processes; manage systemd services
- 🐳 **Docker Integration**: Manage containers, view stats, stream logs
- 🗄️ **Database Management**: Existing relational/document engines plus private Qdrant deployment, collection management, diagnostics and snapshots
- ⚖️ **Nginx Load Balancers**: Guided setup, validated deployment, rollback, backend health and safe editing
- 🤖 **MCP Integration**: Structured tools, scoped tokens, durable operations, audit and dashboard approvals
- 🔐 **Authentication and RBAC**: JWT sessions, current user permissions, scoped server access, and trusted SSH host keys
- ⚡ **High Performance**: Concurrent monitoring with goroutines and in-memory caching
- 📦 **Single Binary**: All assets embedded - no external dependencies
- 💾 **Export/Import Migration**: Pack up all configurations (servers, projects, databases, domains, users) into a single JSON file and restore with automatic SSH key serialization
- 🚀 **Durable GitHub CI/CD**: HMAC-verified, idempotent webhook deliveries with queued deployment history and GitHub Actions CI-first support

## Tech Stack

- **Backend**: Go 1.24+
- **Router**: Chi
- **Frontend**: HTML5 + Tailwind CSS + Vanilla JavaScript
- **WebSockets**: Gorilla WebSocket
- **System Metrics**: gopsutil
- **Docker SDK**: Official Docker client for Go

## Quick Start

### Prerequisites

- Go 1.24 or higher
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

4. Set a persistent random JWT secret and run the application:
```bash
export GOPULSE_AUTH_JWT_SECRET="$(openssl rand -hex 32)"
export GOPULSE_SERVER_HOST=127.0.0.1
./bin/gopulse
```

Save the secret securely for subsequent starts. Configure trusted SSH fingerprints in `GOPULSE_SSH_KNOWN_HOSTS` before using remote management.

The dashboard will be available at `http://localhost:8080`

Create the first administrator through the registration screen on an empty store. There is no default login. Use HTTPS for remote access.

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
  jwt_secret: "" # Set GOPULSE_AUTH_JWT_SECRET instead
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
export GOPULSE_AUTH_JWT_SECRET="$(openssl rand -hex 32)"
export GOPULSE_SSH_KNOWN_HOSTS="/path/to/verified_known_hosts"
# Optional isolated state directory:
export GOPULSE_DATA_DIR="/path/to/dashboard-data"
```

See [the infrastructure guide](docs/infrastructure-management.md) for Qdrant setup, load balancer prerequisites, MCP client connection, permissions, migration changes and test evidence. The [initial audit](docs/infrastructure-audit.md) records the defects found before implementation.

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

#### Export/Import (Admin Only)
- `GET /api/export-import/export` - Export full dashboard data as a downloadable JSON backup
- `GET /api/export-import/export/preview` - Preview record counts for backup estimation
- `POST /api/export-import/import` - Import dashboard configuration backup (supports skip/overwrite resolution modes)

#### GitHub CI/CD
- `POST /api/webhooks/github/{projectID}` - Public HMAC-authenticated GitHub push/ping receiver
- `GET /api/servers/{serverID}/projects/{projectID}/deployments` - List durable deployment attempts
- `GET /api/servers/{serverID}/projects/{projectID}/deployments/{deploymentID}` - Read one deployment status

See [`docs/cicd.md`](docs/cicd.md) for the recommended GitHub Actions workflow and the native webhook alternative.

## Development

### Run in development mode

Create a local secret once, then start the app with hot reload:

```bash
(umask 077; set -C; printf 'GOPULSE_AUTH_JWT_SECRET=%s\n' "$(openssl rand -hex 32)" > .env)
make dev
```

Skip the first command if `.env` already exists. GoPulse loads `.env` from the working directory for `make dev`, `make run`, and direct binary starts. Exported environment variables take precedence. The file is ignored by Git; keep the secret stable across restarts to preserve sessions. Missing or weak secrets still prevent startup.

### Run tests

```bash
make test
node --test web/tests/infrastructure.test.cjs
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

## UI improvement plan

The [core UI user journeys](docs/ui-user-journeys.md) define server onboarding, application deployment, deployment inspection, troubleshooting, and database backup/restore. They describe target interactions, current implementation gaps, and acceptance scenarios for the UI redesign.

The console starts with **Servers** (`/` and `/servers`). Choose a host to open its resource workspace: Overview, Applications, Databases, Domains and TLS, Load balancers, and administrative tools. A server switcher keeps that context explicit. The fleet supports search, workload filters, grid/list layouts, and a workload grouping based on saved applications, databases, and load balancers. These groupings do not imply network connectivity or automatic orchestration. Activity and workspace settings remain secondary destinations. Existing global resource URLs redirect to the relevant server workload filter. Sections use `?view=projects`, `?view=databases`, and other explicit view names so refresh and browser history preserve the screen. Legacy section hashes are accepted. Mobile navigation uses a collapsible drawer and a server-section selector.

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

## Performance and security

Metrics use the existing in-memory collector cache. Fleet capacity and latency have not been benchmarked; earlier numerical performance claims were not supported by this verification run.

Requests enforce current RBAC and server scope. SSH uses strict known_hosts verification. MCP adds tool schemas, rate limits, audit and human approval for disruptive actions. The shared IP limit is 100 requests/minute; MCP also limits each user to 60 requests/minute.

Read [the infrastructure guide](docs/infrastructure-management.md) before upgrading: a configured JWT secret and verified SSH host keys are required. Committed OAuth credentials were removed; rotate the exposed GitHub secret and supply your own environment configuration. Legacy infrastructure credentials and configuration exports remain sensitive and are not encrypted at rest.

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


### Dashboard request and database checks

Page HTML and embedded JS/CSS are served independently of API quotas. API traffic has a 600-request/minute IP ceiling before authentication and a 240-request/minute budget per signed-in user; login and registration share a separate 10-attempt/minute IP bucket. A rejected API request returns JSON with `Retry-After`. Proxy headers do not change the bucket identity.

The server fleet uses one scoped `/api/servers/inventory` snapshot with local resource counts. Opening database inventory or details does not probe every remote engine. The connection-check button performs an authenticated live probe (`?refresh=true`) with a 15-second deadline; transport failures retain the last known state and report the actual verification error. Copying a complete database URI explicitly requests credentials through a `database:write`-protected, non-cacheable endpoint. Inventory responses continue to omit secrets.

Air watches `.js` and `.css` alongside Go/templates because these assets are embedded in the server binary. Restart the development watcher after changing its configuration. Startup checks existing application runtimes before attempting automatic starts, so healthy PM2 processes are not recreated merely because the dashboard restarts.
