package services

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func backupKeyPath(directory, id string) string { return filepath.Join(directory, id+".agekey") }
func createBackupIdentity(directory, id string) (string, error) {
	if _, err := uuidBackupID(id); err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(backupKeyPath(directory, id), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := io.WriteString(file, identity.String()+"\n")
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		os.Remove(backupKeyPath(directory, id))
		return "", err
	}
	return identity.Recipient().String(), nil
}
func readBackupIdentity(directory string, plan models.RemoteBackupPlan) (*age.X25519Identity, error) {
	if _, err := uuidBackupID(plan.ID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(backupKeyPath(directory, plan.ID))
	if err != nil {
		return nil, fmt.Errorf("recovery key unavailable; restore the plan's exported key before running backups")
	}
	identity, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
	if err != nil || identity.Recipient().String() != plan.Recipient {
		return nil, fmt.Errorf("recovery key does not match this backup plan")
	}
	return identity, nil
}

// Encrypted bundles use standard age and ZIP formats. Source dumps are streamed,
// bounded, and checked for successful command completion before publication.
func encryptBackupBundle(dump io.ReadCloser, manifest models.BackupManifest, recipient string, maxBytes int64) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		var finalErr error
		defer func() { dump.Close(); writer.CloseWithError(finalErr) }()
		target, err := age.ParseX25519Recipient(recipient)
		if err != nil {
			finalErr = err
			return
		}
		encrypted, err := age.Encrypt(writer, target)
		if err != nil {
			finalErr = err
			return
		}
		archive := zip.NewWriter(encrypted)
		header, err := archive.Create("manifest.json")
		if err != nil {
			finalErr = err
			return
		}
		if err = json.NewEncoder(header).Encode(manifest); err != nil {
			finalErr = err
			return
		}
		payload, err := archive.Create("database.dump")
		if err != nil {
			finalErr = err
			return
		}
		count, err := io.Copy(payload, io.LimitReader(dump, maxBytes+1))
		if err != nil {
			finalErr = fmt.Errorf("source dump did not complete successfully")
			return
		}
		if count == 0 || count > maxBytes {
			finalErr = fmt.Errorf("database dump is empty or exceeds the plan size limit")
			return
		}
		if err = archive.Close(); err != nil {
			finalErr = err
			return
		}
		finalErr = encrypted.Close()
	}()
	return reader
}

type digestReader struct {
	source       io.Reader
	digest       hash.Hash
	expected     string
	expectedSize int64
	count        int64
	checked      bool
	checkErr     error
}

func (r *digestReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.count += int64(n)
	r.digest.Write(p[:n])
	if r.count > r.expectedSize {
		return n, fmt.Errorf("encrypted backup exceeds recorded size")
	}
	if err == io.EOF && !r.checked {
		r.checked = true
		if r.count != r.expectedSize || hex.EncodeToString(r.digest.Sum(nil)) != r.expected {
			r.checkErr = fmt.Errorf("encrypted backup checksum or size does not match")
			return n, r.checkErr
		}
	}
	if r.checkErr != nil {
		return n, r.checkErr
	}
	return n, err
}

type authenticatedBackupReader struct {
	plain  io.Reader
	cipher *digestReader
}

func (r *authenticatedBackupReader) Read(p []byte) (int, error) {
	n, err := r.plain.Read(p)
	if err == io.EOF {
		if _, checkErr := io.Copy(io.Discard, r.cipher); checkErr != nil {
			return n, checkErr
		}
		if !r.cipher.checked {
			return n, fmt.Errorf("encrypted backup verification incomplete")
		}
	}
	return n, err
}
func decryptVerifiedBackup(source io.Reader, identity *age.X25519Identity, a models.RemoteBackupArtifact) (io.Reader, error) {
	digest := &digestReader{source: source, digest: sha256.New(), expected: a.SHA256, expectedSize: a.Size}
	plain, err := age.Decrypt(digest, identity)
	if err != nil {
		return nil, fmt.Errorf("backup authentication failed")
	}
	return &authenticatedBackupReader{plain: plain, cipher: digest}, nil
}
