package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

// ProjectService manages project lifecycle
type ProjectService struct {
	store       *store.ProjectStore
	domainStore *store.DomainStore
	deployments *store.DeploymentStore
	manager     *ServiceManager
	dataPath    string
}

// ErrWebhookRequiresGitSource prevents a signed event from being accepted for
// a project that GoPulse cannot update from the repository that emitted it.
var ErrWebhookRequiresGitSource = errors.New("webhook requires a project imported from a Git repository with a source URL")

var ErrProjectAlreadyConnectedToGit = errors.New("project is already connected to a Git repository")

func projectWebhookReady(project models.Project) bool {
	return strings.EqualFold(strings.TrimSpace(project.SourceType), "git") &&
		strings.TrimSpace(project.SourceURL) != ""
}

func normalizeGitHubRepositoryURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", fmt.Errorf("repository URL must be an HTTPS GitHub clone URL")
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("repository URL must use github.com/owner/repository format")
	}
	return fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1]), nil
}

func authenticatedGitHubRepositoryURL(repositoryURL string, token string) (string, error) {
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("GitHub access token is required")
	}
	parsed, err := url.Parse(repositoryURL)
	if err != nil {
		return "", err
	}
	parsed.User = url.UserPassword("oauth2", token)
	return parsed.String(), nil
}

func sanitizeGitRemoteURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	parsed.User = nil
	return parsed.String()
}

func redactSecret(value string, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

// NewProjectService creates a new ProjectService
func NewProjectService(store *store.ProjectStore, manager *ServiceManager, dataPath string) *ProjectService {
	return &ProjectService{
		store:    store,
		manager:  manager,
		dataPath: dataPath,
	}
}

// SetDomainStore allows project integrations to resolve domain mappings.
func (s *ProjectService) SetDomainStore(domainStore *store.DomainStore) {
	s.domainStore = domainStore
}

// SetDeploymentStore enables durable deployment history in the release view.
func (s *ProjectService) SetDeploymentStore(deploymentStore *store.DeploymentStore) {
	s.deployments = deploymentStore
}

// resolveRemoteProjectPath determines the project path on the remote server
// For root: /root/projects/{type}/{name}
// For non-root: /home/{user}/projects/{type}/{name}
func (s *ProjectService) resolveRemoteProjectPath(serverID string, projectType string, projectName string) (string, error) {
	client, err := s.manager.GetSSHClient(serverID)
	if err != nil {
		return "", err
	}

	// Get the remote user's home directory
	homeDir, err := client.RunCommand("echo $HOME")
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		homeDir = "/root" // Fallback
	}

	return fmt.Sprintf("%s/projects/%s/%s", homeDir, projectType, projectName), nil
}

// ListProjects returns all projects
func (s *ProjectService) ListProjects(ctx context.Context) ([]models.Project, error) {
	return s.store.ListProjects(), nil
}

// CreateProject creates a new project record and potentially directories
func (s *ProjectService) CreateProject(ctx context.Context, project models.Project) error {
	// 1. Set default path if empty — resolve on the remote server
	if project.Path == "" {
		remotePath, err := s.resolveRemoteProjectPath(project.ServerID, project.Type, project.Name)
		if err != nil {
			return fmt.Errorf("failed to resolve project path: %w", err)
		}
		project.Path = remotePath
	}

	project.Status = models.ProjectStatusStopped

	// 2. Save metadata
	if err := s.store.AddProject(project); err != nil {
		return err
	}

	// 3. Create directory on server
	if err := s.ensureDirectory(project); err != nil {
		s.store.RemoveProject(project.ID)
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	return nil
}

func (s *ProjectService) ensureDirectory(project models.Project) error {
	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("mkdir -p %s", project.Path)
	_, err = client.RunCommand(cmd)
	return err
}

// SetupProject handles git clone or setting up files
func (s *ProjectService) SetupProject(ctx context.Context, projectID string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return err
	}

	if project.SourceType == "git" && project.SourceURL != "" {
		// Clone git repo
		// Check if directory is empty or just clone into it
		// git clone <url> <path> . (dash dot? no, just path)
		// If path exists and is empty, git clone <url> . inside path
		cmd := fmt.Sprintf("git clone %s %s", project.SourceURL, project.Path)
		// If path needs to be empty, we might need to handle that.
		// For now simple clone.
		if _, err := client.RunCommand(cmd); err != nil {
			return fmt.Errorf("failed to clone repo: %w", err)
		}
	}

	// If upload, files are expected to be there (handled by upload handler).

	// Sync env vars to the cloned git directory
	_ = s.SyncEnvFile(ctx, projectID)

	return nil
}

// UploadProjectFile uploads and extracts a project file
func (s *ProjectService) UploadProjectFile(ctx context.Context, projectID string, fileContent io.Reader, fileName string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return err
	}

	// Use RemoteFileService for file operations
	fileService, err := NewRemoteFileService(client)
	if err != nil {
		return err
	}
	defer fileService.Close()
	// 0. Ensure unzip is available
	if err := s.ensureUnzipInstalled(client); err != nil {
		// Log warning but proceed? Or fail?
		// If we fail here, we save the user from a slow upload/failure later.
		// But maybe they want to rely on the fallback?
		// The user explicitly wants speed, so let's try to enforce it or at least warn.
		// For now we just return error if we can't ensure it, unless we want to fallback silently.
		// Let's log it and continue, allowing fallback to native decompress if command fails.
		// Actually, if this fails, Decompress will try native.
	}

	// 1. Upload zip to temp location
	tempPath := fmt.Sprintf("/tmp/%s_%s", project.ID, fileName)
	if err := fileService.WriteFile(ctx, tempPath, fileContent, 0644); err != nil {
		return fmt.Errorf("failed to upload project file: %w", err)
	}
	defer fileService.Delete(ctx, tempPath)

	// 2. Ensure project dir exists
	if err := fileService.Mkdir(ctx, project.Path); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	// 3. Unzip
	if err := fileService.Decompress(ctx, tempPath, project.Path); err != nil {
		return fmt.Errorf("failed to decompress project file: %w", err)
	}

	// 4. Flatten directory if needed
	// Check if only one directory exists (typical with zip files)
	lsCmd := fmt.Sprintf("ls -A %s", project.Path)
	output, err := client.RunCommand(lsCmd)
	if err == nil {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) == 1 {
			// Check if it is a directory
			singleItem := lines[0]
			fullPath := fmt.Sprintf("%s/%s", project.Path, singleItem)
			checkDirCmd := fmt.Sprintf("if [ -d \"%s\" ]; then echo \"yes\"; else echo \"no\"; fi", fullPath)
			isDir, _ := client.RunCommand(checkDirCmd)

			if strings.TrimSpace(isDir) == "yes" {
				// Move contents up
				// mv /path/to/project/subdir/* /path/to/project/
				// mv /path/to/project/subdir/.* /path/to/project/ (for hidden files)
				// rm -rf /path/to/project/subdir

				moveCmd := fmt.Sprintf("mv %s/* %s/ && mv %s/.* %s/ 2>/dev/null; rmdir %s", fullPath, project.Path, fullPath, project.Path, fullPath)
				client.RunCommand(moveCmd)
			}
		}
	}

	// Sync env vars to the new files directory
	_ = s.SyncEnvFile(ctx, projectID)

	return nil
}

// DetectBuildStrategy determines if we should use Docker or Process
func (s *ProjectService) DetectBuildStrategy(ctx context.Context, projectID string) (string, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return "", err
	}

	// Check for files
	cmd := fmt.Sprintf("ls %s", project.Path)
	output, err := client.RunCommand(cmd)
	if err != nil {
		return "", err
	}

	files := strings.Split(output, "\n")
	hasDockerfile := false
	hasCompose := false
	hasPackageJson := false

	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "Dockerfile" {
			hasDockerfile = true
		}
		if f == "docker-compose.yml" || f == "docker-compose.yaml" {
			hasCompose = true
		}
		if f == "package.json" {
			hasPackageJson = true
		}
	}

	if hasDockerfile || hasCompose {
		return "docker", nil
	}
	if hasPackageJson {
		return "process", nil
	}

	return "unknown", nil
}

// ensureNodeInstalled checks for node/npm and installs via nvm if missing
func (s *ProjectService) ensureNodeInstalled(client *remote.SSHClient) error {
	// Check if node is available
	// We need to source nvm if it exists, because non-interactive shells might not have it.
	checkCmd := "export NVM_DIR=\"$HOME/.nvm\" && [ -s \"$NVM_DIR/nvm.sh\" ] && \\. \"$NVM_DIR/nvm.sh\"; node -v"
	if _, err := client.RunCommand(checkCmd); err == nil {
		return nil // Node is installed
	}

	// Install NVM if not present
	// Check if nvm dir exists
	checkNvmCmd := "[ -d \"$HOME/.nvm\" ] && echo 'exists'"
	out, _ := client.RunCommand(checkNvmCmd)
	if strings.TrimSpace(out) != "exists" {
		// Install NVM
		installNvm := "curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.7/install.sh | bash"
		if _, err := client.RunCommand(installNvm); err != nil {
			return fmt.Errorf("failed to install nvm: %w", err)
		}
	}

	// Install Node via NVM
	installNode := "export NVM_DIR=\"$HOME/.nvm\" && [ -s \"$NVM_DIR/nvm.sh\" ] && \\. \"$NVM_DIR/nvm.sh\" && nvm install --lts"
	if _, err := client.RunCommand(installNode); err != nil {
		return fmt.Errorf("failed to install node: %w", err)
	}

	return nil
}

// getNodeCommandPrefix returns the prefix to source nvm
func (s *ProjectService) getNodeCommandPrefix() string {
	return "export NVM_DIR=\"$HOME/.nvm\" && if [ -s \"$NVM_DIR/nvm.sh\" ]; then \\. \"$NVM_DIR/nvm.sh\"; fi && "
}

const laravelUIPresetStartCommand = "php artisan serve --port=8000 --host=0.0.0.0"

func normalizeLaravelStartCommand(startCommand string, port int) string {
	startCommand = strings.TrimSpace(startCommand)
	if startCommand == "" || startCommand == laravelUIPresetStartCommand {
		return fmt.Sprintf("php artisan serve --port=%d --host=0.0.0.0", port)
	}
	return startCommand
}

func projectShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func pm2ShellStartCommand(projectName string, startCommand string) string {
	return fmt.Sprintf(
		"pm2 start /bin/sh --name %s -- -c %s",
		projectShellQuote(projectName),
		projectShellQuote("exec "+startCommand),
	)
}

// findProjectRoot looks for package.json to determine the actual project root
func (s *ProjectService) findProjectRoot(client *remote.SSHClient, basePath string) (string, error) {
	// Check if package.json exists in basePath
	checkCmd := fmt.Sprintf("[ -f \"%s/package.json\" ] && echo 'found'", basePath)
	out, _ := client.RunCommand(checkCmd)
	if strings.TrimSpace(out) == "found" {
		return basePath, nil
	}

	// If not found, look in subdirectories (maxdepth 2)
	// find basePath -maxdepth 2 -name package.json
	findCmd := fmt.Sprintf("find %s -maxdepth 2 -name package.json | head -n 1", basePath)
	out, err := client.RunCommand(findCmd)
	if err == nil && out != "" {
		// /path/to/subdir/package.json -> /path/to/subdir
		fullPath := strings.TrimSpace(out)
		// Get dir name
		// We can use unix dirname
		dirCmd := fmt.Sprintf("dirname \"%s\"", fullPath)
		dirOut, err := client.RunCommand(dirCmd)
		if err == nil {
			return strings.TrimSpace(dirOut), nil
		}
	}

	return basePath, nil // Default to base path if not found
}

// findGoProjectRoot looks for go.mod or main.go to determine the actual Go project root
func (s *ProjectService) findGoProjectRoot(client *remote.SSHClient, basePath string) (string, error) {
	// Check if go.mod exists in basePath
	checkCmd := fmt.Sprintf("[ -f \"%s/go.mod\" ] && echo 'found'", basePath)
	out, _ := client.RunCommand(checkCmd)
	if strings.TrimSpace(out) == "found" {
		return basePath, nil
	}

	// If not found, look for go.mod in subdirectories (maxdepth 2)
	findCmd := fmt.Sprintf("find %s -maxdepth 2 -name go.mod | head -n 1", basePath)
	out, err := client.RunCommand(findCmd)
	if err == nil && strings.TrimSpace(out) != "" {
		fullPath := strings.TrimSpace(out)
		dirCmd := fmt.Sprintf("dirname \"%s\"", fullPath)
		dirOut, err := client.RunCommand(dirCmd)
		if err == nil && strings.TrimSpace(dirOut) != "" {
			return strings.TrimSpace(dirOut), nil
		}
	}

	// If go.mod is not found at all, look for main.go
	checkMainCmd := fmt.Sprintf("[ -f \"%s/main.go\" ] && echo 'found'", basePath)
	outMain, _ := client.RunCommand(checkMainCmd)
	if strings.TrimSpace(outMain) == "found" {
		return basePath, nil
	}

	findMainCmd := fmt.Sprintf("find %s -maxdepth 2 -name main.go | head -n 1", basePath)
	outMain2, err := client.RunCommand(findMainCmd)
	if err == nil && strings.TrimSpace(outMain2) != "" {
		fullPath := strings.TrimSpace(outMain2)
		dirCmd := fmt.Sprintf("dirname \"%s\"", fullPath)
		dirOut, err := client.RunCommand(dirCmd)
		if err == nil && strings.TrimSpace(dirOut) != "" {
			return strings.TrimSpace(dirOut), nil
		}
	}

	return basePath, nil
}

// BuildProject executes the build
func (s *ProjectService) BuildProject(ctx context.Context, projectID string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return err
	}

	project.Status = models.ProjectStatusBuilding
	s.store.UpdateProject(project)

	if project.BuildType == "process" {
		if project.Type == "go" {
			// Ensure Go is installed
			if err := s.ensureGoInstalled(client); err != nil {
				project.Status = models.ProjectStatusError
				s.store.UpdateProject(project)
				return fmt.Errorf("failed to ensure go environment: %w", err)
			}

			// Find actual root (where go.mod or main.go is)
			workDir, err := s.findGoProjectRoot(client, project.Path)
			if err != nil {
				workDir = project.Path
			}

			// Run build command
			buildCommand := project.BuildCommand
			if buildCommand == "" {
				buildCommand = "go build -o app"
			}

			// If build command is default "go build -o app", check for root Go files to prevent "no Go files" error
			if buildCommand == "go build -o app" {
				checkGoFiles := fmt.Sprintf("find %s -maxdepth 1 -name \"*.go\" | grep -q . && echo \"yes\" || echo \"no\"", workDir)
				hasGoFilesOut, _ := client.RunCommand(checkGoFiles)
				if strings.TrimSpace(hasGoFilesOut) != "yes" {
					// Look for main.go in subdirectories
					findMain := fmt.Sprintf("find %s -name \"main.go\" | head -n 1", workDir)
					mainFileOut, err := client.RunCommand(findMain)
					if err == nil && strings.TrimSpace(mainFileOut) != "" {
						mainFilePath := strings.TrimSpace(mainFileOut)
						buildCommand = fmt.Sprintf("go build -o app %s", mainFilePath)
					}
				}
			}

			cmdPrefix := s.getGoCommandPrefix()
			// Dependencies
			if _, err := client.RunCommand(fmt.Sprintf("%scd %s && go mod download", cmdPrefix, workDir)); err != nil {
				// Ignore error, maybe no go.mod
			}

			cmd := fmt.Sprintf("%scd %s && %s", cmdPrefix, workDir, buildCommand)
			if out, err := client.RunCommand(cmd); err != nil {
				project.Status = models.ProjectStatusError
				s.store.UpdateProject(project)
				return fmt.Errorf("build failed: %s\n%w", out, err)
			}
		} else if project.Type == "laravel" {
			// Ensure PHP & Composer are installed
			if err := s.ensureLaravelInstalled(client); err != nil {
				project.Status = models.ProjectStatusError
				s.store.UpdateProject(project)
				return fmt.Errorf("failed to ensure Laravel environment: %w", err)
			}

			workDir := project.Path

			// Run build command
			if project.BuildCommand == "" {
				project.BuildCommand = "composer install --no-dev --optimize-autoloader"
			}

			cmd := fmt.Sprintf("cd %s && %s", workDir, project.BuildCommand)
			if out, err := client.RunCommand(cmd); err != nil {
				project.Status = models.ProjectStatusError
				s.store.UpdateProject(project)
				return fmt.Errorf("build failed: %s\n%w", out, err)
			}
		} else {
			// Node/Next.js Logic
			// Ensure Node is installed
			if err := s.ensureNodeInstalled(client); err != nil {
				project.Status = models.ProjectStatusError
				s.store.UpdateProject(project)
				return fmt.Errorf("failed to ensure node environment: %w", err)
			}

			// Find actual root
			workDir, _ := s.findProjectRoot(client, project.Path)

			// Ensure Swap for OOM protection
			if err := s.ensureSwap(client); err != nil {
				// Log but proceed? Better to fail if critical, but maybe user has their own swap.
				// Let's log (if we had a logger) and proceed, or just return error.
				// For now, let's treat it as non-fatal but highly recommended.
				// fmt.Printf("Warning: failed to setup swap: %v\n", err)
			}

			// Run build command
			if project.BuildCommand == "" {
				if project.Type == "nextjs" {
					project.BuildCommand = "npm install && npm run build"
				} else {
					project.BuildCommand = "npm install"
				}
			}

			// Prefix with NVM source and Memory Limit
			// Add NODE_OPTIONS for memory limit (e.g. 2GB) to prevent aggressive GC or allocation beyond swap
			// Actually, standard Node limit is 1.5GB roughly on 64bit.
			// Let's set it to 3GB if we added 2GB swap + assume 1GB RAM = 3GB total.
			// Safer: --max-old-space-size=2048 (2GB)
			cmdPrefix := s.getNodeCommandPrefix()
			buildEnv := "export NODE_OPTIONS='--max-old-space-size=2048' && "

			cmd := fmt.Sprintf("%s%scd %s && %s", cmdPrefix, buildEnv, workDir, project.BuildCommand)
			if _, err := client.RunCommand(cmd); err != nil {
				project.Status = models.ProjectStatusError
				s.store.UpdateProject(project)
				return fmt.Errorf("build failed: %w", err)
			}
		}
	} else if project.BuildType == "docker" {
		// Docker build logic
		// docker build -t project-name .
		cmd := fmt.Sprintf("cd %s && docker build -t %s .", project.Path, strings.ToLower(project.Name))
		if _, err := client.RunCommand(cmd); err != nil {
			project.Status = models.ProjectStatusError
			s.store.UpdateProject(project)
			return fmt.Errorf("docker build failed: %w", err)
		}
	}

	project.LastBuild = time.Now()
	project.Status = models.ProjectStatusStopped
	return s.store.UpdateProject(project)
}

// ensurePortOpen allows the port through UFW
func (s *ProjectService) ensurePortOpen(client *remote.SSHClient, port int) error {
	// Check if UFW is active
	checkCmd := "sudo ufw status | grep -q 'Status: active' && echo 'active' || echo 'inactive'"
	out, _ := client.RunCommand(checkCmd)
	if strings.TrimSpace(out) == "active" {
		// Allow port
		cmd := fmt.Sprintf("sudo ufw allow %d/tcp", port)
		if _, err := client.RunCommand(cmd); err != nil {
			return fmt.Errorf("failed to allow port %d: %w", port, err)
		}
	}
	return nil
}

// ensurePortClosed denies/deletes the port rule from UFW
func (s *ProjectService) ensurePortClosed(client *remote.SSHClient, port int) error {
	// Check if UFW is active
	checkCmd := "sudo ufw status | grep -q 'Status: active' && echo 'active' || echo 'inactive'"
	out, _ := client.RunCommand(checkCmd)
	if strings.TrimSpace(out) == "active" {
		// Delete allow rule
		// Run twice to ensure both IPv4 and IPv6 rules are removed if they exist separately
		cmd := fmt.Sprintf("sudo ufw delete allow %d/tcp", port)

		client.RunCommand(cmd)

	}
	return nil
}

// StartProject starts the application
func (s *ProjectService) StartProject(ctx context.Context, projectID string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return err
	}

	// Ensure port is open
	if err := s.ensurePortOpen(client, project.Port); err != nil {
		// Log warning but maybe proceed? Or fail?
		// Better to fail if we can't expose it.
		// fmt.Printf("Warning: failed to open port: %v\n", err)
	}

	if project.BuildType == "process" {
		if project.Type == "go" {
			if project.StartCommand == "" {
				project.StartCommand = "./app"
			}

			// Ensure pm2 (for process management)
			// We can use pm2 to manage binary too: pm2 start ./app --name app-name
			cmdPrefix := s.getGoCommandPrefix()

			// Check pm2
			nodePrefix := s.getNodeCommandPrefix() // Need node for pm2
			checkPm2 := fmt.Sprintf("%spm2 -v", nodePrefix)
			if _, err := client.RunCommand(checkPm2); err != nil {
				// Install PM2 using npm
				// Ensure node first
				s.ensureNodeInstalled(client)
				installPm2 := fmt.Sprintf("%snpm install -g pm2", nodePrefix)
				client.RunCommand(installPm2)
			}

			// Start with pm2
			// pm2 start ./app --name "name" --no-autorestart (maybe?)
			// Need execution permissions
			workDir, err := s.findGoProjectRoot(client, project.Path)
			if err != nil {
				workDir = project.Path
			}
			chmodCmd := fmt.Sprintf("chmod +x %s/%s", workDir, strings.Split(project.StartCommand, " ")[0])
			client.RunCommand(chmodCmd)

			client.RunCommand(fmt.Sprintf("%spm2 delete \"%s\"", nodePrefix, project.Name))
			cmd := fmt.Sprintf("%s%scd %s && pm2 start %s --name \"%s\"", nodePrefix, cmdPrefix, workDir, project.StartCommand, project.Name)
			if _, err := client.RunCommand(cmd); err != nil {
				return fmt.Errorf("start failed: %w", err)
			}

		} else if project.Type == "laravel" {
			if err := s.ensureLaravelInstalled(client); err != nil {
				return fmt.Errorf("failed to ensure Laravel environment: %w", err)
			}

			project.StartCommand = normalizeLaravelStartCommand(project.StartCommand, project.Port)

			// Ensure PM2 is installed
			nodePrefix := s.getNodeCommandPrefix()
			checkPm2 := fmt.Sprintf("%spm2 -v", nodePrefix)
			if _, err := client.RunCommand(checkPm2); err != nil {
				s.ensureNodeInstalled(client)
				installPm2 := fmt.Sprintf("%snpm install -g pm2", nodePrefix)
				client.RunCommand(installPm2)
			}

			workDir := project.Path
			client.RunCommand(fmt.Sprintf("%spm2 delete %s", nodePrefix, projectShellQuote(project.Name)))
			cmd := fmt.Sprintf("%scd %s && %s", nodePrefix, projectShellQuote(workDir), pm2ShellStartCommand(project.Name, project.StartCommand))
			if out, err := client.RunCommand(cmd); err != nil {
				return fmt.Errorf("start failed: %s\n%w", strings.TrimSpace(out), err)
			}
		} else {
			// Node/Next.js Logic
			// Use PM2 or Systemd?
			// For simplicity, let's try PM2 if available, or nohup.
			// User mentioned "we could ask user to connect github... then we build".
			// Let's assume we want a persistent process.
			// "pm2 start npm --name <name> -- start"
			// Or "npm start"

			if project.StartCommand == "" {
				project.StartCommand = "npm start"
			}

			// Ensure Node is installed
			if err := s.ensureNodeInstalled(client); err != nil {
				return fmt.Errorf("failed to ensure node environment: %w", err)
			}

			// Also ensure pm2 is installed
			cmdPrefix := s.getNodeCommandPrefix()
			checkPm2 := fmt.Sprintf("%spm2 -v", cmdPrefix)
			if _, err := client.RunCommand(checkPm2); err != nil {
				// Install PM2
				installPm2 := fmt.Sprintf("%snpm install -g pm2", cmdPrefix)
				client.RunCommand(installPm2)
			}

			// Find actual root
			workDir, _ := s.findProjectRoot(client, project.Path)

			client.RunCommand(fmt.Sprintf("%spm2 delete \"%s\"", cmdPrefix, project.Name))
			// Simple pm2 check/start
			// pm2 start npm --name "app-name" -- start
			cmd := fmt.Sprintf("%scd %s && pm2 start npm --name \"%s\" -- start", cmdPrefix, workDir, project.Name)

			// If custom start command is just "node something.js", we can use pm2 start something.js
			if strings.HasPrefix(project.StartCommand, "node ") {
				script := strings.TrimPrefix(project.StartCommand, "node ")
				cmd = fmt.Sprintf("%scd %s && pm2 start %s --name \"%s\"", cmdPrefix, workDir, script, project.Name)
			}

			// For now let's stick to npm start via pm2 for generic support
			if _, err := client.RunCommand(cmd); err != nil {
				return fmt.Errorf("start failed: %w", err)
			}
		}

		// Get Process ID from pm2?
		// We can just mark as running.
	} else if project.BuildType == "docker" {
		// docker run -d -p HostPort:Port --name Name Image
		// Stop existing if any
		client.RunCommand(fmt.Sprintf("docker stop %s && docker rm %s", project.Name, project.Name))

		restartPolicy := ""
		if project.RestartOnBoot {
			restartPolicy = "--restart unless-stopped "
		}

		cmd := fmt.Sprintf("docker run -d %s-p %d:%d --name %s %s", restartPolicy, project.HostPort, project.Port, project.Name, strings.ToLower(project.Name))
		output, err := client.RunCommand(cmd)
		if err != nil {
			return fmt.Errorf("docker run failed: %w", err)
		}
		project.ContainerID = strings.TrimSpace(output)
	}

	project.Status = models.ProjectStatusRunning
	return s.store.UpdateProject(project)
}

// StopProject stops the application
func (s *ProjectService) StopProject(ctx context.Context, projectID string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return err
	}

	if project.BuildType == "process" {
		cmdPrefix := s.getNodeCommandPrefix()
		cmd := fmt.Sprintf("%spm2 stop %s", cmdPrefix, project.Name)
		client.RunCommand(cmd)
	} else if project.BuildType == "docker" {
		cmd := fmt.Sprintf("docker stop %s", project.Name)
		client.RunCommand(cmd)
	}

	project.Status = models.ProjectStatusStopped
	return s.store.UpdateProject(project)

}

// DeleteProject deletes the project
func (s *ProjectService) DeleteProject(ctx context.Context, projectID string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err == nil {
		// Stop and cleanup
		s.StopProject(ctx, projectID)

		// Close port
		s.ensurePortClosed(client, project.Port)

		if project.BuildType == "process" {
			cmdPrefix := s.getNodeCommandPrefix()
			client.RunCommand(fmt.Sprintf("%spm2 delete %s", cmdPrefix, project.Name))
		} else if project.BuildType == "docker" {
			client.RunCommand(fmt.Sprintf("docker rm %s", project.Name))
		}

		// Remove files on remote server
		if project.Path != "" && strings.Contains(project.Path, "/projects/") {
			client.RunCommand(fmt.Sprintf("rm -rf %s", project.Path))
		}
	}

	return s.store.RemoveProject(projectID)
}

// GetProject returns project details
func (s *ProjectService) GetProject(ctx context.Context, id string) (models.Project, error) {
	return s.store.GetProject(id)
}

// ensureUnzipInstalled checks for unzip and installs it if missing
func (s *ProjectService) ensureUnzipInstalled(client *remote.SSHClient) error {
	checkCmd := "command -v unzip"
	if _, err := client.RunCommand(checkCmd); err == nil {
		return nil
	}

	// Try apt-get (Debian/Ubuntu)
	if _, err := client.RunCommand("which apt-get"); err == nil {
		// Update and install
		// DEBIAN_FRONTEND=noninteractive to avoid prompts
		cmd := "export DEBIAN_FRONTEND=noninteractive && sudo apt-get update && sudo apt-get install -y unzip"
		if _, err := client.RunCommand(cmd); err != nil {
			return fmt.Errorf("failed to install unzip via apt-get: %w", err)
		}
		return nil
	}

	// Try apk (Alpine)
	if _, err := client.RunCommand("which apk"); err == nil {
		if _, err := client.RunCommand("sudo apk add unzip"); err != nil {
			return fmt.Errorf("failed to install unzip via apk: %w", err)
		}
		return nil
	}

	// Try yum (CentOS/RHEL)
	if _, err := client.RunCommand("which yum"); err == nil {
		if _, err := client.RunCommand("sudo yum install -y unzip"); err != nil {
			return fmt.Errorf("failed to install unzip via yum: %w", err)
		}
		return nil
	}

	return fmt.Errorf("unzip command not found and package manager detection failed")
}

// ensureSwap checks for swap and adds 2GB if missing
func (s *ProjectService) ensureSwap(client *remote.SSHClient) error {
	// Check if swap is active
	checkSwap := "swapon --show"
	out, err := client.RunCommand(checkSwap)
	if err == nil && strings.TrimSpace(out) != "" {
		// Swap exists
		return nil
	}

	// Create swap
	// 1. fallocate
	// 2. chmod
	// 3. mkswap
	// 4. swapon
	cmd := `
		sudo fallocate -l 2G /swapfile && \
		sudo chmod 600 /swapfile && \
		sudo mkswap /swapfile && \
		sudo swapon /swapfile && \
		echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
	`
	// Check if fallocate exists, if not use dd
	checkFallocate := "command -v fallocate"
	if _, err := client.RunCommand(checkFallocate); err != nil {
		cmd = `
			sudo dd if=/dev/zero of=/swapfile bs=1024 count=2097152 && \
			sudo chmod 600 /swapfile && \
			sudo mkswap /swapfile && \
			sudo swapon /swapfile && \
			echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
		`
	}

	if _, err := client.RunCommand(cmd); err != nil {
		return fmt.Errorf("failed to create swap: %w", err)
	}

	return nil
}

// ensureGoInstalled checks for Go and installs it if missing
func (s *ProjectService) ensureGoInstalled(client *remote.SSHClient) error {
	// Check if go is available
	if _, err := client.RunCommand("go version"); err == nil {
		return nil
	}

	// Install Go
	// 1. Download tar.gz
	// 2. Remove old installation
	// 3. Extract to /usr/local/go
	// 4. Update path
	goVer := "1.22.0"
	url := fmt.Sprintf("https://go.dev/dl/go%s.linux-amd64.tar.gz", goVer)

	cmd := fmt.Sprintf(`
		curl -L %s -o /tmp/go.tar.gz && \
		sudo rm -rf /usr/local/go && \
		sudo tar -C /usr/local -xzf /tmp/go.tar.gz && \
		rm /tmp/go.tar.gz && \
		echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile && \
		echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
	`, url)

	if _, err := client.RunCommand(cmd); err != nil {
		return fmt.Errorf("failed to install go: %w", err)
	}

	return nil
}

// ensureLaravelInstalled checks for PHP and Composer and installs them if missing
func (s *ProjectService) ensureLaravelInstalled(client *remote.SSHClient) error {
	// Check if php is available
	if _, err := client.RunCommand("php -v"); err == nil {
		// Check if composer is available
		if _, err := client.RunCommand("composer --version"); err == nil {
			return nil // Both are installed
		}
	}

	// Install PHP and extensions if missing
	installCmd := "export DEBIAN_FRONTEND=noninteractive && (which apt-get && apt-get update && apt-get install -y php php-cli php-mbstring php-xml php-bcmath php-curl php-zip unzip curl || sudo apt-get update && sudo apt-get install -y php php-cli php-mbstring php-xml php-bcmath php-curl php-zip unzip curl)"
	if _, err := client.RunCommand(installCmd); err != nil {
		return fmt.Errorf("failed to install php and extensions: %w", err)
	}

	// Install Composer
	composerCmd := "curl -sS https://getcomposer.org/installer | php && (mv composer.phar /usr/local/bin/composer || sudo mv composer.phar /usr/local/bin/composer)"
	if _, err := client.RunCommand(composerCmd); err != nil {
		// Log warning but proceed if composer already exists or let it fail downstream
	}

	return nil
}

// getGoCommandPrefix returns the prefix to ensure go is in path
func (s *ProjectService) getGoCommandPrefix() string {
	return "export PATH=$PATH:/usr/local/go/bin && "
}

// UpdateProject updates project configuration
func (s *ProjectService) UpdateProject(ctx context.Context, project models.Project) error {
	existing, err := s.store.GetProject(project.ID)
	if err != nil {
		return err
	}

	// Update allowed fields
	existing.Name = project.Name
	existing.Port = project.Port
	existing.HostPort = project.HostPort
	existing.BuildCommand = project.BuildCommand
	existing.StartCommand = project.StartCommand
	existing.RestartOnBoot = project.RestartOnBoot

	// Start/Build type usually immutable or requires more logic, sticking to commands for now.
	// But let's allow updating type/build_type if needed, user might want to switch strategies.
	if project.BuildType != "" {
		existing.BuildType = project.BuildType
	}

	return s.store.UpdateProject(existing)
}

// CreateProjectFromRepo creates a new project by cloning a git repository on the remote server
func (s *ProjectService) CreateProjectFromRepo(ctx context.Context, project models.Project, repoURL string, token string) (*models.Project, error) {
	// 1. Set project metadata
	project.SourceType = "git"
	project.SourceURL = repoURL
	project.Status = "stopped"
	project.CreatedAt = time.Now()

	// 2. Determine remote project path based on user's home directory
	projectPath, err := s.resolveRemoteProjectPath(project.ServerID, project.Type, project.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project path: %w", err)
	}
	project.Path = projectPath

	// 3. Save project record
	if err := s.store.AddProject(project); err != nil {
		return nil, fmt.Errorf("failed to create project record: %w", err)
	}

	// 4. Get SSH client for the remote server
	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		s.store.RemoveProject(project.ID)
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	// 5. Create directory on remote server
	mkdirCmd := fmt.Sprintf("mkdir -p %s", projectPath)
	if _, err := client.RunCommand(mkdirCmd); err != nil {
		s.store.RemoveProject(project.ID)
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	// 6. Clone repository on remote server via SSH
	authRepoURL := repoURL
	if token != "" {
		if strings.HasPrefix(repoURL, "https://") {
			authRepoURL = strings.Replace(repoURL, "https://", fmt.Sprintf("https://oauth2:%s@", token), 1)
		}
	}

	cloneCmd := fmt.Sprintf("git clone %s %s", authRepoURL, projectPath)
	if output, err := client.RunCommand(cloneCmd); err != nil {
		client.RunCommand(fmt.Sprintf("rm -rf %s", projectPath))
		s.store.RemoveProject(project.ID)
		return nil, fmt.Errorf("git clone failed: %s: %w", output, err)
	}

	return &project, nil
}

// ConnectProjectToGitHub converts an uploaded project into a Git-backed
// project without replacing its currently deployed files. For a directory
// that is not already a repository, it creates a local snapshot first. The
// first CI/CD deployment can therefore roll back to the uploaded version if
// the selected GitHub revision fails to build or start.
func (s *ProjectService) ConnectProjectToGitHub(ctx context.Context, projectID string, repoURL string, token string, defaultBranch string) (*models.Project, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if projectWebhookReady(project) {
		return nil, ErrProjectAlreadyConnectedToGit
	}
	if s.manager == nil {
		return nil, fmt.Errorf("server connection manager is unavailable")
	}
	if strings.TrimSpace(project.Path) == "" {
		return nil, fmt.Errorf("project path is not configured")
	}

	canonicalURL, err := normalizeGitHubRepositoryURL(repoURL)
	if err != nil {
		return nil, err
	}
	authenticatedURL, err := authenticatedGitHubRepositoryURL(canonicalURL, token)
	if err != nil {
		return nil, err
	}
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	if !validGitBranch(defaultBranch) {
		return nil, fmt.Errorf("invalid GitHub default branch %q", defaultBranch)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	path := projectShellQuote(project.Path)
	if out, err := client.RunCommand(fmt.Sprintf("test -d %s", path)); err != nil {
		return nil, fmt.Errorf("project directory is unavailable: %s: %w", strings.TrimSpace(out), err)
	}

	// Verify access before modifying the project directory. The authenticated
	// URL is used only for this command and is never persisted in project data.
	preflight := fmt.Sprintf("git ls-remote --exit-code %s %s", projectShellQuote(authenticatedURL), projectShellQuote("refs/heads/"+defaultBranch))
	if out, err := client.RunCommand(preflight); err != nil {
		return nil, fmt.Errorf("cannot read the selected GitHub repository or default branch: %s: %w", redactSecret(strings.TrimSpace(out), token), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	gitState, _ := client.RunCommand(fmt.Sprintf("cd %s && if [ -d .git ]; then printf 'git=1\nremote=%%s\nhead=%%s\n' \"$(git config --get remote.origin.url 2>/dev/null)\" \"$(git rev-parse --verify HEAD 2>/dev/null)\"; else printf 'git=0\n'; fi", path))
	state := map[string]string{}
	for _, line := range strings.Split(gitState, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			state[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if existingRemote := state["remote"]; existingRemote != "" {
		existingCanonical, normalizeErr := normalizeGitHubRepositoryURL(sanitizeGitRemoteURL(existingRemote))
		if normalizeErr != nil || !strings.EqualFold(existingCanonical, canonicalURL) {
			return nil, fmt.Errorf("project directory is already linked to a different Git remote")
		}
	}

	if state["git"] != "1" {
		initCommand := fmt.Sprintf(
			"cd %s && git init && git config user.name %s && git config user.email %s && git checkout -B %s && git add -A && git commit --allow-empty -m %s",
			path,
			projectShellQuote("GoPulse"),
			projectShellQuote("gopulse@localhost"),
			projectShellQuote("gopulse-upload-backup"),
			projectShellQuote("GoPulse snapshot before GitHub connection"),
		)
		if out, err := client.RunCommand(initCommand); err != nil {
			return nil, fmt.Errorf("failed to snapshot uploaded project before connecting GitHub: %s: %w", strings.TrimSpace(out), err)
		}
	} else if state["head"] == "" {
		snapshotCommand := fmt.Sprintf(
			"cd %s && git config user.name %s && git config user.email %s && git checkout -B %s && git add -A && git commit --allow-empty -m %s",
			path,
			projectShellQuote("GoPulse"),
			projectShellQuote("gopulse@localhost"),
			projectShellQuote("gopulse-upload-backup"),
			projectShellQuote("GoPulse snapshot before GitHub connection"),
		)
		if out, err := client.RunCommand(snapshotCommand); err != nil {
			return nil, fmt.Errorf("failed to snapshot uploaded project before connecting GitHub: %s: %w", strings.TrimSpace(out), err)
		}
	}

	credentialPath := project.Path + "/.git/gopulse-credentials"
	remoteCommand := fmt.Sprintf(
		"cd %s && if git remote get-url origin >/dev/null 2>&1; then git remote set-url origin %s; else git remote add origin %s; fi && umask 077 && printf '%%s\\n' %s > %s && git config credential.helper %s && git fetch --prune origin %s",
		path,
		projectShellQuote(canonicalURL),
		projectShellQuote(canonicalURL),
		projectShellQuote(authenticatedURL),
		projectShellQuote(credentialPath),
		projectShellQuote("store --file="+credentialPath),
		projectShellQuote(defaultBranch),
	)
	if out, err := client.RunCommand(remoteCommand); err != nil {
		return nil, fmt.Errorf("failed to configure the GitHub remote: %s: %w", redactSecret(strings.TrimSpace(out), token), err)
	}

	project.SourceType = "git"
	project.SourceURL = canonicalURL
	if err := s.store.UpdateProject(project); err != nil {
		return nil, fmt.Errorf("failed to save GitHub repository connection: %w", err)
	}
	return &project, nil
}

// RedeployProject performs a deployment from the repository's current branch.
func (s *ProjectService) RedeployProject(ctx context.Context, projectID string) error {
	return s.RedeployProjectRevision(ctx, projectID, "", "")
}

var (
	gitCommitPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	gitBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

func validGitBranch(branch string) bool {
	return gitBranchPattern.MatchString(branch) &&
		!strings.Contains(branch, "..") &&
		!strings.Contains(branch, "//") &&
		!strings.Contains(branch, "@{") &&
		!strings.HasSuffix(branch, "/") &&
		!strings.HasSuffix(branch, ".") &&
		!strings.HasSuffix(branch, ".lock")
}

func (s *ProjectService) restoreProjectRevision(client *remote.SSHClient, project models.Project, revision string) error {
	if revision == "" {
		return fmt.Errorf("previous revision is unavailable")
	}
	cmd := fmt.Sprintf("cd %s && git reset --hard %s", projectShellQuote(project.Path), projectShellQuote(revision))
	if out, err := client.RunCommand(cmd); err != nil {
		return fmt.Errorf("git rollback failed: %s: %w", strings.TrimSpace(out), err)
	}
	return nil
}

func (s *ProjectService) restoreProjectStatus(projectID string, status models.ProjectStatus) {
	latest, err := s.store.GetProject(projectID)
	if err != nil {
		return
	}
	latest.Status = status
	_ = s.store.UpdateProject(latest)
}

// RedeployProjectRevision deploys an exact push revision when supplied. Source
// and build preparation happen before the live process is stopped; if the new
// runtime cannot start, GoPulse makes a best-effort rollback to the prior commit.
func (s *ProjectService) RedeployProjectRevision(ctx context.Context, projectID string, branch string, commitSHA string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}
	previousStatus := project.Status

	if err := ctx.Err(); err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}

	previousRevision := ""
	if project.SourceType == "git" && project.Path != "" {
		out, _ := client.RunCommand(fmt.Sprintf("cd %s && git rev-parse HEAD", projectShellQuote(project.Path)))
		previousRevision = strings.TrimSpace(out)

		branch = strings.TrimSpace(branch)
		commitSHA = strings.TrimSpace(commitSHA)
		var sourceCmd string
		switch {
		case branch != "" || commitSHA != "":
			if !validGitBranch(branch) {
				return fmt.Errorf("invalid deployment branch %q", branch)
			}
			if !gitCommitPattern.MatchString(commitSHA) {
				return fmt.Errorf("invalid deployment commit %q", commitSHA)
			}
			sourceCmd = fmt.Sprintf(
				"cd %s && git fetch --prune origin %s && git checkout -B %s %s",
				projectShellQuote(project.Path),
				projectShellQuote(branch),
				projectShellQuote(branch),
				projectShellQuote(commitSHA),
			)
		default:
			sourceCmd = fmt.Sprintf("cd %s && git pull --ff-only", projectShellQuote(project.Path))
		}
		if out, err := client.RunCommand(sourceCmd); err != nil {
			return fmt.Errorf("source update failed: %s: %w", strings.TrimSpace(out), err)
		}
		if commitSHA != "" {
			out, err := client.RunCommand(fmt.Sprintf("cd %s && git rev-parse HEAD", projectShellQuote(project.Path)))
			if err != nil || !strings.EqualFold(strings.TrimSpace(out), commitSHA) {
				_ = s.restoreProjectRevision(client, project, previousRevision)
				return fmt.Errorf("checked out revision does not match requested commit %s", commitSHA)
			}
		}
	} else {
		return fmt.Errorf("project is not configured with a Git source")
	}

	if err := ctx.Err(); err != nil {
		_ = s.restoreProjectRevision(client, project, previousRevision)
		s.restoreProjectStatus(projectID, previousStatus)
		return err
	}

	// Prepare configuration and build while the previous runtime is still alive.
	if err := s.SyncEnvFile(ctx, projectID); err != nil {
		_ = s.restoreProjectRevision(client, project, previousRevision)
		return fmt.Errorf("environment sync failed during redeploy: %w", err)
	}
	if err := s.BuildProject(ctx, projectID); err != nil {
		rollbackErr := s.restoreProjectRevision(client, project, previousRevision)
		s.restoreProjectStatus(projectID, previousStatus)
		if rollbackErr != nil {
			return fmt.Errorf("build failed during redeploy: %w; source rollback also failed: %v", err, rollbackErr)
		} else {
			return fmt.Errorf("build failed during redeploy; previous runtime was left running: %w", err)
		}
	}

	if err := ctx.Err(); err != nil {
		_ = s.restoreProjectRevision(client, project, previousRevision)
		s.restoreProjectStatus(projectID, previousStatus)
		return err
	}

	// Cut over only after a successful build.
	if err := s.StopProject(ctx, projectID); err != nil {
		_ = s.restoreProjectRevision(client, project, previousRevision)
		s.restoreProjectStatus(projectID, previousStatus)
		return fmt.Errorf("stop failed during redeploy: %w", err)
	}
	if err := s.StartProject(ctx, projectID); err != nil {
		startErr := err
		rollbackErr := s.restoreProjectRevision(client, project, previousRevision)
		if rollbackErr == nil {
			if buildErr := s.BuildProject(ctx, projectID); buildErr == nil {
				if restartErr := s.StartProject(ctx, projectID); restartErr == nil {
					return fmt.Errorf("new deployment failed to start and was rolled back: %w", startErr)
				}
			}
		}
		return fmt.Errorf("start failed during redeploy: %w; automatic rollback failed: %v", startErr, rollbackErr)
	}

	// Persist success only after the new runtime is running.
	project, _ = s.store.GetProject(projectID)
	project.LastDeploy = time.Now().UTC()
	return s.store.UpdateProject(project)
}

// GetProjectReleaseInfo returns build/deploy/source activity for the operations dialog.
func (s *ProjectService) GetProjectReleaseInfo(ctx context.Context, projectID string) (models.ProjectReleaseInfo, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return models.ProjectReleaseInfo{}, err
	}

	info := models.ProjectReleaseInfo{
		ProjectID: project.ID,
		Releases:  []models.ProjectReleaseEvent{},
	}

	if project.Path != "" && s.manager != nil {
		if client, err := s.manager.GetSSHClient(project.ServerID); err == nil {
			info.Git = s.getProjectGitInfo(client, project.Path)
		}
	}

	hasMatchingLastDeploy := false
	if s.deployments != nil {
		deployments, listErr := s.deployments.ListProjectDeployments(projectID, 50)
		if listErr == nil {
			for _, deployment := range deployments {
				timestamp := deployment.QueuedAt
				if !deployment.FinishedAt.IsZero() {
					timestamp = deployment.FinishedAt
				} else if !deployment.StartedAt.IsZero() {
					timestamp = deployment.StartedAt
				}
				commit := deployment.CommitSHA
				if len(commit) > 12 {
					commit = commit[:12]
				}
				detail := strings.TrimSpace(strings.Join([]string{commit, deployment.CommitMessage}, " "))
				if deployment.Error != "" {
					detail = deployment.Error
				}
				if detail == "" {
					detail = deployment.Source
				}
				title := "Deploy"
				if commit != "" {
					title = fmt.Sprintf("Deploy %s", commit)
				}
				if deployment.Status == models.DeploymentStatusSucceeded &&
					!project.LastDeploy.IsZero() && !deployment.FinishedAt.IsZero() {
					delta := deployment.FinishedAt.Sub(project.LastDeploy)
					if delta < 0 {
						delta = -delta
					}
					if delta <= time.Second {
						hasMatchingLastDeploy = true
					}
				}
				info.Releases = append(info.Releases, models.ProjectReleaseEvent{
					ID:        deployment.ID,
					Type:      "deploy",
					Status:    string(deployment.Status),
					Title:     title,
					Detail:    detail,
					Timestamp: timestamp,
				})
			}
		}
	}

	if !hasMatchingLastDeploy && projectTimestampValid(project.LastDeploy) {
		info.Releases = append(info.Releases, models.ProjectReleaseEvent{
			ID:        "last-deploy",
			Type:      "deploy",
			Status:    "completed",
			Title:     "Last Deploy",
			Detail:    "Webhook or redeploy cycle completed",
			Timestamp: project.LastDeploy,
		})
	}

	if projectTimestampValid(project.LastBuild) {
		info.Releases = append(info.Releases, models.ProjectReleaseEvent{
			ID:        "last-build",
			Type:      "build",
			Status:    "completed",
			Title:     "Last Build",
			Detail:    project.BuildCommand,
			Timestamp: project.LastBuild,
		})
	}

	if info.Git.IsRepository && info.Git.Commit != "" {
		detail := info.Git.Message
		if detail == "" {
			detail = "Current source revision"
		}
		info.Releases = append(info.Releases, models.ProjectReleaseEvent{
			ID:        "current-source",
			Type:      "source",
			Status:    "current",
			Title:     fmt.Sprintf("Commit %s", info.Git.Commit),
			Detail:    detail,
			Timestamp: time.Now(),
		})
	}

	if projectTimestampValid(project.CreatedAt) {
		info.Releases = append(info.Releases, models.ProjectReleaseEvent{
			ID:        "created",
			Type:      "created",
			Status:    "completed",
			Title:     "Project Created",
			Detail:    project.Path,
			Timestamp: project.CreatedAt,
		})
	}

	sort.SliceStable(info.Releases, func(i, j int) bool {
		return info.Releases[i].Timestamp.After(info.Releases[j].Timestamp)
	})

	return info, nil
}

// GetProjectLogs returns recent runtime logs from PM2 or Docker.
func (s *ProjectService) GetProjectLogs(ctx context.Context, projectID string, tail int) (models.ProjectLogInfo, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return models.ProjectLogInfo{}, err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return models.ProjectLogInfo{}, err
	}

	tail = clampProjectLogTail(tail)
	source := "pm2"
	var cmd string
	if project.BuildType == "docker" {
		source = "docker"
		target := project.ContainerID
		if strings.TrimSpace(target) == "" {
			target = project.Name
		}
		cmd = fmt.Sprintf("docker logs --tail %d --timestamps %s 2>&1", tail, projectShellQuote(target))
	} else {
		cmd = fmt.Sprintf("%spm2 logs %s --nostream --lines %d 2>&1", s.getNodeCommandPrefix(), projectShellQuote(project.Name), tail)
	}

	raw, runErr := client.RunCommand(cmd)
	if runErr != nil && strings.TrimSpace(raw) == "" {
		return models.ProjectLogInfo{}, fmt.Errorf("failed to fetch %s logs: %w", source, runErr)
	}
	if runErr != nil {
		raw = strings.TrimRight(raw, "\n") + "\n" + runErr.Error()
	}

	return models.ProjectLogInfo{
		ProjectID: project.ID,
		Source:    source,
		Tail:      tail,
		Raw:       raw,
		Lines:     splitProjectLogLines(raw),
		FetchedAt: time.Now(),
	}, nil
}

// CheckProjectHealth runs operational checks against the project runtime.
func (s *ProjectService) CheckProjectHealth(ctx context.Context, projectID string) (models.ProjectHealthReport, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return models.ProjectHealthReport{}, err
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return models.ProjectHealthReport{}, err
	}

	expectedRunning := strings.EqualFold(string(project.Status), string(models.ProjectStatusRunning))
	checks := []models.ProjectHealthCheck{}

	pathOut, _ := client.RunCommand(fmt.Sprintf("test -d %s && echo ok || echo missing", projectShellQuote(project.Path)))
	if strings.TrimSpace(pathOut) == "ok" {
		checks = append(checks, models.ProjectHealthCheck{Name: "Project Path", Status: "healthy", Message: "Directory exists", Detail: project.Path})
	} else {
		checks = append(checks, models.ProjectHealthCheck{Name: "Project Path", Status: "unhealthy", Message: "Directory is missing", Detail: project.Path})
	}

	checks = append(checks, s.checkProjectRuntime(client, project, expectedRunning))

	if project.Port > 0 {
		portCmd := fmt.Sprintf("if command -v ss >/dev/null 2>&1; then ss -ltn | awk '{print $4}' | grep -Eq '(:|\\])%[1]d$'; elif command -v lsof >/dev/null 2>&1; then lsof -nP -iTCP:%[1]d -sTCP:LISTEN >/dev/null 2>&1; else nc -z 127.0.0.1 %[1]d >/dev/null 2>&1; fi && echo listening || echo closed", project.Port)
		portOut, _ := client.RunCommand(portCmd)
		if strings.TrimSpace(portOut) == "listening" {
			checks = append(checks, models.ProjectHealthCheck{Name: "Port", Status: "healthy", Message: fmt.Sprintf("Port %d is listening", project.Port)})
		} else if expectedRunning {
			checks = append(checks, models.ProjectHealthCheck{Name: "Port", Status: "unhealthy", Message: fmt.Sprintf("Port %d is not listening", project.Port)})
		} else {
			checks = append(checks, models.ProjectHealthCheck{Name: "Port", Status: "warning", Message: fmt.Sprintf("Port %d is closed while project is not running", project.Port)})
		}

		probeCmd := fmt.Sprintf("if command -v curl >/dev/null 2>&1; then code=$(curl -sS -m 5 -o /dev/null -w '%%{http_code}' http://127.0.0.1:%d/ 2>/dev/null || true); echo ${code:-000}; else echo no-curl; fi", project.Port)
		probeOut, _ := client.RunCommand(probeCmd)
		code := strings.TrimSpace(probeOut)
		switch {
		case code == "no-curl":
			checks = append(checks, models.ProjectHealthCheck{Name: "HTTP Probe", Status: "warning", Message: "curl is not installed on the remote server"})
		case code >= "200" && code <= "399":
			checks = append(checks, models.ProjectHealthCheck{Name: "HTTP Probe", Status: "healthy", Message: fmt.Sprintf("HTTP %s from localhost", code)})
		case code >= "400" && code <= "599":
			checks = append(checks, models.ProjectHealthCheck{Name: "HTTP Probe", Status: "warning", Message: fmt.Sprintf("Application responded with HTTP %s", code)})
		case expectedRunning:
			checks = append(checks, models.ProjectHealthCheck{Name: "HTTP Probe", Status: "unhealthy", Message: "No HTTP response from localhost"})
		default:
			checks = append(checks, models.ProjectHealthCheck{Name: "HTTP Probe", Status: "warning", Message: "No HTTP response while project is not running"})
		}
	} else {
		checks = append(checks, models.ProjectHealthCheck{Name: "Port", Status: "warning", Message: "No project port configured"})
	}

	if project.SourceType == "git" || project.SourceURL != "" {
		git := s.getProjectGitInfo(client, project.Path)
		if git.IsRepository {
			checks = append(checks, models.ProjectHealthCheck{Name: "Git", Status: "healthy", Message: "Repository available", Detail: git.Commit})
		} else {
			checks = append(checks, models.ProjectHealthCheck{Name: "Git", Status: "warning", Message: "Project is marked as Git source but no repository was detected"})
		}
	}

	return models.ProjectHealthReport{
		ProjectID: project.ID,
		Status:    projectHealthStatus(checks),
		CheckedAt: time.Now(),
		Checks:    checks,
	}, nil
}

// GetProjectAutomation returns currently supported automation settings.
func (s *ProjectService) GetProjectAutomation(ctx context.Context, projectID string, webhookURL string) (models.ProjectAutomationInfo, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return models.ProjectAutomationInfo{}, err
	}

	return models.ProjectAutomationInfo{
		ProjectID:      project.ID,
		RestartOnBoot:  project.RestartOnBoot,
		WebhookEnabled: project.WebhookEnabled,
		WebhookReady:   projectWebhookReady(project),
		WebhookURL:     webhookURL,
		WebhookSecret:  project.WebhookSecret,
		RepositoryURL:  project.SourceURL,
		UpdatedAt:      time.Now(),
	}, nil
}

// UpdateProjectAutomation updates currently supported automation settings.
func (s *ProjectService) UpdateProjectAutomation(ctx context.Context, projectID string, update models.ProjectAutomationUpdate, webhookURL string) (models.ProjectAutomationInfo, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return models.ProjectAutomationInfo{}, err
	}

	if update.RestartOnBoot != nil {
		project.RestartOnBoot = *update.RestartOnBoot
	}

	if update.WebhookEnabled != nil {
		if *update.WebhookEnabled {
			if !projectWebhookReady(project) {
				return models.ProjectAutomationInfo{}, ErrWebhookRequiresGitSource
			}
			if !project.WebhookEnabled || project.WebhookSecret == "" {
				secret, err := GenerateWebhookSecret()
				if err != nil {
					return models.ProjectAutomationInfo{}, fmt.Errorf("failed to generate webhook secret: %w", err)
				}
				project.WebhookSecret = secret
			}
			project.WebhookEnabled = true
		} else {
			project.WebhookEnabled = false
			project.WebhookSecret = ""
		}
	}

	if err := s.store.UpdateProject(project); err != nil {
		return models.ProjectAutomationInfo{}, err
	}

	return models.ProjectAutomationInfo{
		ProjectID:      project.ID,
		RestartOnBoot:  project.RestartOnBoot,
		WebhookEnabled: project.WebhookEnabled,
		WebhookReady:   projectWebhookReady(project),
		WebhookURL:     webhookURL,
		WebhookSecret:  project.WebhookSecret,
		RepositoryURL:  project.SourceURL,
		UpdatedAt:      time.Now(),
	}, nil
}

// GetProjectIntegrations returns project-adjacent dashboard integrations.
func (s *ProjectService) GetProjectIntegrations(ctx context.Context, projectID string, webhookURL string) (models.ProjectIntegrationsInfo, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return models.ProjectIntegrationsInfo{}, err
	}

	envCount := len(project.EnvVars)
	domains := s.getProjectDomains(project)
	domainNames := make([]string, 0, len(domains))
	for _, domain := range domains {
		domainNames = append(domainNames, domain.Domain)
	}

	domainStatus := "unconfigured"
	domainMessage := "No domain linked"
	domainData := map[string]interface{}{
		"domain":  project.Domain,
		"domains": domainNames,
		"port":    project.Port,
	}
	if len(domainNames) > 0 {
		domainStatus = "configured"
		domainMessage = strings.Join(domainNames, ", ")
		domainData["domain"] = domainNames[0]
	} else if project.Domain != "" {
		domainStatus = "configured"
		domainMessage = project.Domain
		domainData["domain"] = project.Domain
	}

	webhookStatus := "disabled"
	if project.WebhookEnabled {
		webhookStatus = "enabled"
	}

	return models.ProjectIntegrationsInfo{
		ProjectID: project.ID,
		Integrations: []models.ProjectIntegration{
			{
				Key:     "files",
				Label:   "Files",
				Status:  "available",
				Message: project.Path,
				Data:    map[string]interface{}{"path": project.Path},
			},
			{
				Key:     "environment",
				Label:   "Environment",
				Status:  "available",
				Message: fmt.Sprintf("%d variables", envCount),
				Data:    map[string]interface{}{"count": envCount},
			},
			{
				Key:     "webhook",
				Label:   "Webhook",
				Status:  webhookStatus,
				Message: webhookURL,
				Data:    map[string]interface{}{"url": webhookURL, "enabled": project.WebhookEnabled},
			},
			{
				Key:     "domain",
				Label:   "Domain",
				Status:  domainStatus,
				Message: domainMessage,
				Data:    domainData,
			},
			{
				Key:     "database",
				Label:   "Database",
				Status:  "available",
				Message: "Open database clusters",
				Data:    map[string]interface{}{"project": project.Name},
			},
			{
				Key:     "terminal",
				Label:   "Terminal",
				Status:  "available",
				Message: project.Path,
				Data:    map[string]interface{}{"path": project.Path},
			},
		},
	}, nil
}

func (s *ProjectService) getProjectGitInfo(client *remote.SSHClient, path string) models.ProjectGitInfo {
	info := models.ProjectGitInfo{}
	if strings.TrimSpace(path) == "" {
		return info
	}

	cmd := fmt.Sprintf("cd %s 2>/dev/null && if [ -d .git ]; then printf 'commit=%%s\\nbranch=%%s\\nmessage=%%s\\nremote=%%s\\n' \"$(git rev-parse --short HEAD 2>/dev/null)\" \"$(git rev-parse --abbrev-ref HEAD 2>/dev/null)\" \"$(git log -1 --pretty=%%s 2>/dev/null)\" \"$(git config --get remote.origin.url 2>/dev/null)\"; fi", projectShellQuote(path))
	out, err := client.RunCommand(cmd)
	if err != nil && strings.TrimSpace(out) == "" {
		return info
	}

	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	info.Commit = values["commit"]
	info.Branch = values["branch"]
	info.Message = values["message"]
	info.Remote = sanitizeGitRemoteURL(values["remote"])
	info.IsRepository = info.Commit != "" || info.Branch != "" || info.Remote != ""
	return info
}

func (s *ProjectService) getProjectDomains(project models.Project) []models.Domain {
	if s.domainStore == nil || project.ServerID == "" {
		return []models.Domain{}
	}

	ports := map[int]bool{}
	if project.Port > 0 {
		ports[project.Port] = true
	}
	if project.HostPort > 0 {
		ports[project.HostPort] = true
	}

	if len(ports) == 0 {
		return []models.Domain{}
	}

	matches := []models.Domain{}
	for _, domain := range s.domainStore.ListDomainsByServer(project.ServerID) {
		if ports[domain.Port] {
			matches = append(matches, domain)
		}
	}
	return matches
}

func (s *ProjectService) checkProjectRuntime(client *remote.SSHClient, project models.Project, expectedRunning bool) models.ProjectHealthCheck {
	if project.BuildType == "docker" {
		target := project.ContainerID
		if strings.TrimSpace(target) == "" {
			target = project.Name
		}
		out, _ := client.RunCommand(fmt.Sprintf("docker inspect -f '{{.State.Running}}' %s 2>/dev/null || echo false", projectShellQuote(target)))
		if strings.TrimSpace(out) == "true" {
			return models.ProjectHealthCheck{Name: "Runtime", Status: "healthy", Message: "Docker container is running", Detail: target}
		}
		if expectedRunning {
			return models.ProjectHealthCheck{Name: "Runtime", Status: "unhealthy", Message: "Docker container is not running", Detail: target}
		}
		return models.ProjectHealthCheck{Name: "Runtime", Status: "warning", Message: "Docker container is stopped", Detail: target}
	}

	out, _ := client.RunCommand(fmt.Sprintf("%spm2 pid %s 2>/dev/null || echo 0", s.getNodeCommandPrefix(), projectShellQuote(project.Name)))
	pid := strings.TrimSpace(out)
	if parsed, err := strconv.Atoi(pid); err == nil && parsed > 0 {
		return models.ProjectHealthCheck{Name: "Runtime", Status: "healthy", Message: "PM2 process is running", Detail: pid}
	}
	if expectedRunning {
		return models.ProjectHealthCheck{Name: "Runtime", Status: "unhealthy", Message: "PM2 process is not running"}
	}
	return models.ProjectHealthCheck{Name: "Runtime", Status: "warning", Message: "PM2 process is stopped"}
}

func projectTimestampValid(t time.Time) bool {
	return !t.IsZero() && t.Year() > 1
}

func clampProjectLogTail(tail int) int {
	if tail < 20 {
		return 100
	}
	if tail > 1000 {
		return 1000
	}
	return tail
}

func splitProjectLogLines(raw string) []string {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return []string{}
	}
	return strings.Split(raw, "\n")
}

func projectHealthStatus(checks []models.ProjectHealthCheck) string {
	status := "healthy"
	for _, check := range checks {
		switch check.Status {
		case "unhealthy":
			return "unhealthy"
		case "warning":
			status = "warning"
		}
	}
	return status
}

// GenerateWebhookSecret generates a random 32-byte hex string for webhook HMAC verification
func GenerateWebhookSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// EnableWebhook generates a secret and enables the webhook for a project
func (s *ProjectService) EnableWebhook(ctx context.Context, projectID string) (string, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	if !projectWebhookReady(project) {
		return "", ErrWebhookRequiresGitSource
	}

	secret, err := GenerateWebhookSecret()
	if err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}

	project.WebhookSecret = secret
	project.WebhookEnabled = true
	if err := s.store.UpdateProject(project); err != nil {
		return "", err
	}

	return secret, nil
}

// DisableWebhook disables the webhook for a project
func (s *ProjectService) DisableWebhook(ctx context.Context, projectID string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	project.WebhookEnabled = false
	project.WebhookSecret = ""
	return s.store.UpdateProject(project)
}

// parseEnvContent parses raw .env file contents into a key-value map
func parseEnvContent(content string) map[string]string {
	vars := make(map[string]string)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Strip surrounding quotes if present
		if len(val) >= 2 {
			if (strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) ||
				(strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) {
				val = val[1 : len(val)-1]
			}
		}
		vars[key] = val
	}
	return vars
}

// GetEnvVars returns the environment variables for a project, syncing with remote .env if available
func (s *ProjectService) GetEnvVars(ctx context.Context, projectID string) (map[string]string, error) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}

	// Proactively sync from remote .env file if it exists, to allow loading manually uploaded environment variables
	if project.Path != "" {
		client, err := s.manager.GetSSHClient(project.ServerID)
		if err == nil {
			cmd := fmt.Sprintf("cat %s/.env 2>/dev/null", project.Path)
			output, err := client.RunCommand(cmd)
			if err == nil {
				remoteVars := parseEnvContent(output)

				// Update metadata store if there are any differences
				changed := false
				if project.EnvVars == nil {
					project.EnvVars = make(map[string]string)
					changed = true
				}

				// Merge remote keys
				for k, v := range remoteVars {
					if project.EnvVars[k] != v {
						project.EnvVars[k] = v
						changed = true
					}
				}

				// Remove deleted keys
				for k := range project.EnvVars {
					if _, exists := remoteVars[k]; !exists {
						delete(project.EnvVars, k)
						changed = true
					}
				}

				if changed {
					_ = s.store.UpdateProject(project)
				}
				return project.EnvVars, nil
			}
		}
	}

	if project.EnvVars == nil {
		return map[string]string{}, nil
	}
	return project.EnvVars, nil
}

// UpdateEnvVars updates the environment variables for a project and syncs to remote
func (s *ProjectService) UpdateEnvVars(ctx context.Context, projectID string, vars map[string]string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	project.EnvVars = vars
	if err := s.store.UpdateProject(project); err != nil {
		return err
	}

	// Sync to remote server
	return s.SyncEnvFile(ctx, projectID)
}

// SyncEnvFile writes the project's env vars to a .env file on the remote server
func (s *ProjectService) SyncEnvFile(ctx context.Context, projectID string) error {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}

	if project.Path == "" {
		return nil
	}

	client, err := s.manager.GetSSHClient(project.ServerID)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	// If no variables are defined, delete/truncate the .env file
	if len(project.EnvVars) == 0 {
		cmd := fmt.Sprintf("rm -f %s/.env", project.Path)
		if _, err := client.RunCommand(cmd); err != nil {
			return fmt.Errorf("failed to remove .env file: %w", err)
		}
		return nil
	}

	// Build .env content
	var envContent strings.Builder
	for key, value := range project.EnvVars {
		// Escape single quotes in values
		escapedValue := strings.ReplaceAll(value, "'", "'\\''")
		envContent.WriteString(fmt.Sprintf("%s='%s'\n", key, escapedValue))
	}

	// Write to remote .env file
	escaped := strings.ReplaceAll(envContent.String(), "'", "'\\''")
	cmd := fmt.Sprintf("printf '%%s' '%s' > %s/.env", escaped, project.Path)
	if _, err := client.RunCommand(cmd); err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}

	return nil
}

// shouldAutoStart checks the actual runtime before an automatic start. Restarting
// the dashboard must not delete/recreate healthy PM2 processes on managed hosts.
func (s *ProjectService) shouldAutoStart(parent context.Context, project models.Project) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	if project.BuildType == "docker" {
		target := project.ContainerID
		if target == "" {
			target = project.Name
		}
		out, err := s.manager.Run(ctx, project.ServerID, "docker inspect -f '{{.State.Running}}' "+projectShellQuote(target))
		if err != nil {
			return false, err
		}
		switch strings.TrimSpace(out) {
		case "true":
			return false, nil
		case "false":
			return true, nil
		}
		return false, fmt.Errorf("unable to determine container runtime state")
	}
	out, err := s.manager.Run(ctx, project.ServerID, s.getNodeCommandPrefix()+"pm2 pid "+projectShellQuote(project.Name)+" 2>/dev/null")
	if err != nil {
		return false, err
	}
	for _, value := range strings.Fields(out) {
		pid, err := strconv.Atoi(value)
		if err != nil || pid < 0 {
			return false, fmt.Errorf("unable to determine process runtime state")
		}
		if pid > 0 {
			return false, nil
		}
	}
	return true, nil
}

// AutoStartProjects checks for projects with RestartOnBoot enabled and starts them automatically in the background
func (s *ProjectService) AutoStartProjects(ctx context.Context) {
	projects := s.store.ListProjects()
	for _, project := range projects {
		if project.RestartOnBoot {
			go func(proj models.Project) {
				start, err := s.shouldAutoStart(ctx, proj)
				if err != nil {
					fmt.Printf("[GoPulse] Skipping automatic start for '%s': runtime verification unavailable\n", proj.Name)
					return
				}
				if !start {
					return
				}
				fmt.Printf("[GoPulse] Auto-starting project '%s' (ID: %s) on remote server...\n", proj.Name, proj.ID)
				err = s.StartProject(ctx, proj.ID)
				if err != nil {
					fmt.Printf("[GoPulse] Failed to auto-start project '%s': %v\n", proj.Name, err)
				} else {
					fmt.Printf("[GoPulse] Successfully auto-started project '%s'\n", proj.Name)
				}
			}(project)
		}
	}
}
