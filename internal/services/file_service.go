package services

import (
	"context"
	"io"
	"os"
	"time"
)

// FileInfo represents metadata about a file
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
	Path    string    `json:"path"` // Full path
}

// FileService defines operations for file management
type FileService interface {
	// ListDir returns list of files in a directory
	ListDir(ctx context.Context, path string) ([]FileInfo, error)

	// ReadFile returns a reader for the file content
	// The caller is responsible for closing the reader
	ReadFile(ctx context.Context, path string) (io.ReadCloser, error)

	// WriteFile writes content to a file
	// Permission can be specified (e.g. 0644)
	WriteFile(ctx context.Context, path string, content io.Reader, perm os.FileMode) error

	// Delete removes a file or directory (recursive)
	Delete(ctx context.Context, path string) error

	// Mkdir creates a directory (and parents if needed)
	Mkdir(ctx context.Context, path string) error

	// Rename moves/renames a file
	Rename(ctx context.Context, oldPath, newPath string) error

	// Chmod changes file permissions
	Chmod(ctx context.Context, path string, mode os.FileMode) error

	// Compress creates a zip archive of the source path at the destination path
	Compress(ctx context.Context, sourcePath, destPath string) error

	// Decompress extracts a zip archive to the destination path
	Decompress(ctx context.Context, sourcePath, destPath string) error

	// ExecuteCommand runs a shell command in the specified directory
	ExecuteCommand(ctx context.Context, path, command string) (string, error)

	// Close cleans up any resources (like SFTP client)
	Close() error
}
