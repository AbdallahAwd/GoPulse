package store

import (
	"os"
	"path/filepath"
)

// DataDirectory is shared by database state and portable SSH key storage.
func DataDirectory() string {
	if path := os.Getenv("GOPULSE_DATA_DIR"); path != "" {
		return path
	}
	return filepath.Join(os.Getenv("HOME"), ".gopulse")
}
