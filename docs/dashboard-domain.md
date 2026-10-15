# Master dashboard domain

Open **Settings → Master dashboard domain** to configure the address of the GoPulse control plane. This setting applies to the host running GoPulse; it does not select or modify a managed application/database server.

1. Create an A/AAAA DNS record for a dedicated hostname, such as `dashboard.example.com`, pointing to the master host's public address. Open inbound TCP ports 80 and 443 in the host/provider firewall.
2. Enter the hostname and, optionally, the expected public IP. Enable HTTPS, enter a certificate contact email, and accept the Let's Encrypt subscriber agreement.
3. Choose **Save settings**. Saving persists the desired configuration without changing host routing. Expand the Nginx preview to inspect the resulting configuration.
4. On a Linux master host, choose **Set up domain** and review the confirmation. Follow the operation progress. Choose **Check connection** afterwards to verify public DNS, HTTPS, and the identity of this GoPulse instance.

## Host requirements

Automatic deployment requires Linux, systemd, curl, and root or non-interactive sudo. Missing Nginx and Certbot packages are installed with apt on Debian/Ubuntu. On other Linux distributions, install those prerequisites and supply `certbot.timer` before applying. Nginx must include `/etc/nginx/conf.d/*.conf` inside its `http` block. Setup enables the Certbot renewal timer. It does not change firewall rules, the GoPulse bind address, or the application's listen port.

macOS, Windows, and containers without systemd can save/preview the setting, but automatic deployment is disabled. For a reverse proxy in another container/network namespace, adapt the upstream address in the preview to reach the GoPulse process; localhost is relative to the proxy host.

## Behavior and boundaries

- Writes require administrator/full-access permission. Host application additionally requires a dashboard browser session and an explicit approval header; agent bearers cannot apply this setting.
- Hostnames are validated without schemes, paths, ports, wildcards, or IP literals. Public checks reject private, link-local, CGNAT, and documentation IPs, pin connections to resolved addresses, verify TLS normally, and follow no redirects. They send no authentication credentials.
- A random persistent public instance identifier at `/.well-known/gopulse-dashboard` establishes that the hostname reaches this control plane. It exposes no configuration or user information.
- The proxy forwards to the actual GoPulse listener, including IPv6 binds. It preserves WebSocket upgrades and disables buffering for logs/MCP; request uploads are permitted up to 1 GiB at the proxy.
- Setup backs up only its own `/etc/nginx/conf.d/gopulse-dashboard.conf`, refuses symlinks, validates Nginx before reload, and restores the previous dashboard file on certificate/configuration/routing failure when the host command can complete. Other virtual hosts are retained; a conflicting explicit hostname fails before routing changes.
- During certificate bootstrap, HTTP exposes ACME challenges and the public identity probe. Other traffic redirects to HTTPS. Existing dashboard HTTPS server blocks remain present until the replacement configuration is ready.
- Certbot uses `certonly --webroot`, so it does not edit unrelated Nginx sites. Certificates use the `gopulse-dashboard-<hostname>` lineage and record a renewal deploy hook that validates and reloads Nginx.
- No GoPulse, application, or database restart is requested by setup. Nginx reloads after validation; a stopped/new Nginx service may be started. Certificate/package artifacts can remain after a failed operation. Abrupt host shutdown, privileged-process failure, or an unavailable sudo path can prevent rollback; inspect host state before retrying.
- Saved and applied revisions are separate. Changing a setting does not publish it as active. A failed deployment preserves the previous published dashboard/MCP URL. Successfully applying updates the Agents page's generated public MCP endpoint, while existing client configurations are unchanged.
- HTTPS on the applied dashboard hostname uses Secure authentication cookies even when TLS terminates at Nginx. Arbitrary forwarded-protocol headers do not enable this behavior on other hosts.
- The feature never forces all dashboard requests onto a new domain. Direct access remains available. GoPulse settings persist in the existing SQLite `resource_settings` table under a dashboard namespace. Host routing settings and certificate files are not transferred by the application's existing configuration export.

## API

| Endpoint | Purpose |
| --- | --- |
| `GET /api/settings/dashboard/domain` | Saved/applied settings, listener, host platform, published URL |
| `PUT /api/settings/dashboard/domain` | Validate and save the desired hostname/TLS/email/consent/IP |
| `GET /api/settings/dashboard/domain/nginx` | Preview the saved Nginx configuration |
| `POST /api/settings/dashboard/domain/check` | Resolve DNS and verify public dashboard identity/TLS |
| `POST /api/settings/dashboard/domain/apply` | Start a tracked master-host setup operation |

Deployment uses the existing operation queue, eight-minute deadline, resource lock, and durable progress/error reporting. Concurrent settings changes and duplicate deployment starts are blocked while an operation runs. Operations interrupted by a control-plane restart are reported as interrupted instead of replayed.

Proxy behavior follows [Nginx WebSocket guidance](https://nginx.org/en/docs/http/websocket.html) and the [proxy module](https://nginx.org/en/docs/http/ngx_http_proxy_module.html). Certificate setup/renewal follows the [Certbot webroot and deploy-hook guide](https://eff-certbot.readthedocs.io/en/stable/using.html).

Tests use temporary SQLite stores, fake DNS/host executors, and temporary fake Nginx/systemd/Certbot/curl binaries. They execute the deployment script's success and rollback paths without touching real infrastructure. UI verification uses an isolated dashboard with no managed resources.

Validation completed: the full Go test suite, focused domain race checks, `go vet`, all 151 frontend tests, and a Linux build with CGO disabled. Desktop and 390-pixel mobile browser checks covered saving/persistence, configuration preview, hostname validation feedback, and platform gating in an isolated empty dashboard. No real managed application/database or Nginx/certificate deployment was changed while testing.
