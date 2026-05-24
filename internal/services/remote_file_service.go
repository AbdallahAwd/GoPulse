package services

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amaa/gopulse/internal/remote"
)

// RemoteFileService implements FileService using SFTP
type RemoteFileService struct {
	client     *remote.SSHClient
	sftpClient *remote.WrappedSFTPClient
}

// NewRemoteFileService creates a new RemoteFileService
func NewRemoteFileService(client *remote.SSHClient) (*RemoteFileService, error) {
	// We need a dedicated SFTP session. The SSHClient handles the underlying connection.
	// But SSHClient.Connect() handles connection logic.
	// We need access to the underlying *ssh.Client to create an SFTP client.

	// Note: We need to expose the underlying client safely or add a method to creates SFTP client
	// For now, we'll assume we can get it via a new method on SSHClient or access it if we make it public.
	// But SSHClient struct has `client *ssh.Client`.
	// We should add `NewSFTPClient` to `SSHClient`.

	sftpClient, err := client.NewSFTPClient()
	if err != nil {
		return nil, err
	}

	return &RemoteFileService{
		client:     client,
		sftpClient: sftpClient,
	}, nil
}

// ListDir lists files in a directory
func (s *RemoteFileService) ListDir(ctx context.Context, path string) ([]FileInfo, error) {
	entries, err := s.sftpClient.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, entry := range entries {
		mode := entry.Mode().String()
		files = append(files, FileInfo{
			Name:    entry.Name(),
			Size:    entry.Size(),
			Mode:    mode,
			ModTime: entry.ModTime(),
			IsDir:   entry.IsDir(),
			Path:    filepath.Join(path, entry.Name()),
		})
	}

	// Sort: Directories first, then files by name
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir && !files[j].IsDir {
			return true
		}
		if !files[i].IsDir && files[j].IsDir {
			return false
		}
		return files[i].Name < files[j].Name
	})

	return files, nil
}

// ReadFile opens a file for reading
func (s *RemoteFileService) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	return s.sftpClient.Open(path)
}

// WriteFile writes content to a file
func (s *RemoteFileService) WriteFile(ctx context.Context, path string, content io.Reader, perm os.FileMode) error {
	f, err := s.sftpClient.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := s.sftpClient.Chmod(path, perm); err != nil {
		// Try to continue if chmod fails?
	}

	// Use larger buffer for SFTP upload optimization (128KB)
	buf := make([]byte, 128*1024)
	_, err = io.CopyBuffer(f, content, buf)
	return err
}

// Delete removes a file or directory
func (s *RemoteFileService) Delete(ctx context.Context, path string) error {
	// Check if dir
	stat, err := s.sftpClient.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if stat.IsDir() {
		// RemoveDirectory only works on empty dirs.
		// We might need recursive delete.
		// pkg/sftp doesn't have RemoveAll.
		// We can implement recursive walk or just use "rm -rf" via SSH command.
		// Using SSH command is safer/easier for recursive delete.
		_, err := s.client.RunCommand(fmt.Sprintf("rm -rf %q", path))
		return err
	}

	return s.sftpClient.Remove(path)
}

// Mkdir creates a directory
func (s *RemoteFileService) Mkdir(ctx context.Context, path string) error {
	return s.sftpClient.MkdirAll(path)
}

// Rename moves a file
func (s *RemoteFileService) Rename(ctx context.Context, oldPath, newPath string) error {
	return s.sftpClient.Rename(oldPath, newPath)
}

// Chmod changes permissions
func (s *RemoteFileService) Chmod(ctx context.Context, path string, mode os.FileMode) error {
	return s.sftpClient.Chmod(path, mode)
}

// Compress creates a zip archive of the directory or file
// It uses the 'zip' command on the remote server
func (s *RemoteFileService) Compress(ctx context.Context, sourcePath, destPath string) error {
	// Verify source exists
	_, err := s.sftpClient.Stat(sourcePath)
	if err != nil {
		return err
	}

	// We'll use the 'zip' command line tool.
	// zip -r dest.zip source
	// We need to be careful with paths. Ideally we cd to the parent directory of sourcePath
	// so the zip structure is relative.

	parentDir := filepath.Dir(sourcePath)
	baseName := filepath.Base(sourcePath)

	// If source is a file, we just zip it.
	// If source is a dir, -r recurses.

	// Command: cd /parent && zip -r /full/dest.zip baseName
	// Note: destPath should be absolute.

	cmd := fmt.Sprintf("cd %q && zip -r %q %q", parentDir, destPath, baseName)
	_, err = s.client.RunCommand(cmd)
	if err != nil {
		// Fallback to native Go implementation if command fails (e.g. zip not installed)
		// We could check for specific error "127", but trying native is a reasonable fallback for execution errors.
		return s.compressNative(ctx, sourcePath, destPath)
	}
	return nil
}

// compressNative implements zip compression using archive/zip over SFTP
func (s *RemoteFileService) compressNative(ctx context.Context, sourcePath, destPath string) error {
	// Create destination zip file
	archiveFile, err := s.sftpClient.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer archiveFile.Close()

	zipWriter := zip.NewWriter(archiveFile)
	defer zipWriter.Close()

	// Calculate base path to trim from files to make them relative in zip
	baseDir := filepath.Dir(sourcePath)

	walker := s.sftpClient.Walk(sourcePath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}

		info := walker.Stat()
		path := walker.Path()

		// Skip if it tries to include the zip file itself (if inside source)
		if path == destPath {
			continue
		}

		// Create zip header
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		// internal path in zip
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		header.Name = relPath

		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			continue
		}

		// Write file content
		file, err := s.sftpClient.Open(path)
		if err != nil {
			return err
		}

		_, err = io.Copy(writer, file)
		file.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// Decompress extracts a zip archive to the destination path
// It uses the 'unzip' command on the remote server
func (s *RemoteFileService) Decompress(ctx context.Context, sourcePath, destPath string) error {
	// Verify source exists
	_, err := s.sftpClient.Stat(sourcePath)
	if err != nil {
		return err
	}

	// We'll use the 'unzip' command line tool.
	// unzip source.zip -d destPath
	// sourcePath and destPath should be absolute.

	cmd := fmt.Sprintf("unzip %q -d %q", sourcePath, destPath)
	_, err = s.client.RunCommand(cmd)
	if err != nil {
		// Fallback to native implementation
		return s.decompressNative(ctx, sourcePath, destPath)
	}
	return nil
}

// decompressNative implements zip extraction using archive/zip over SFTP
func (s *RemoteFileService) decompressNative(ctx context.Context, sourcePath, destPath string) error {
	// Open source zip file
	zipFile, err := s.sftpClient.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer zipFile.Close()

	stat, err := zipFile.Stat()
	if err != nil {
		return err
	}

	// Create zip reader
	// sftp.File implements io.ReaderAt
	zipReader, err := zip.NewReader(zipFile, stat.Size())
	if err != nil {
		return fmt.Errorf("failed to create zip reader: %w", err)
	}

	for _, f := range zipReader.File {
		// Secure zip slip check
		fpath := filepath.Join(destPath, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(destPath)+string(os.PathSeparator)) {
			// Skip invalid paths
			continue
		}

		if f.FileInfo().IsDir() {
			if err := s.sftpClient.MkdirAll(fpath); err != nil {
				return err
			}
			continue
		}

		// Ensure parent directory exists
		if err := s.sftpClient.MkdirAll(filepath.Dir(fpath)); err != nil {
			return err
		}

		// Create destination file
		destFile, err := s.sftpClient.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			return err
		}

		// Copy content
		rc, err := f.Open()
		if err != nil {
			destFile.Close()
			return err
		}

		_, err = io.Copy(destFile, rc)
		rc.Close()
		destFile.Close()
		if err != nil {
			return err
		}

		// Restore permissions logic could go here, but omitted for simplicity
	}

	return nil
}

// ExecuteCommand runs a shell command in the specified directory
func (s *RemoteFileService) ExecuteCommand(ctx context.Context, path, command string) (string, error) {
	// Simple chaining. Be careful with escaping if command contains special chars that might break out of "sh -c"
	// RunCommand usually executes plain string.
	// We want: cd "path" && command

	// Ensure path is quoted safely or handled. fmt.Sprintf %q does quotes.
	// Command is raw string from user, expected to be a shell command line.

	fullCmd := fmt.Sprintf("cd %q && %s", path, command)
	return s.client.RunCommand(fullCmd)
}

// Close closes the SFTP client
func (s *RemoteFileService) Close() error {
	return s.sftpClient.Close()
}
