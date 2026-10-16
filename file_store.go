package mq

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Compile-time check that FileStore implements SessionStore
var _ SessionStore = (*FileStore)(nil)

// FileStore implements SessionStore using JSON files on disk.
// Each client ID gets its own directory containing separate files for
// pending publishes, subscriptions, and received QoS 2 packet IDs.
//
// File organization:
//
//	baseDir/
//	  clientID/
//	    pending/
//	      1.json
//	      2.json
//	    subscriptions/
//	      <base64_topic>.json
//	    qos2/
//	      1.json
//	      2.json
//
// This implementation is synchronous - all operations block until complete.
// For async/batched writes, wrap it in an AsyncStore.
//
// Each file is written to a temporary file and renamed into place, so a crash
// of the process never leaves a partially written file. The data is not
// fsynced: after a power loss or OS crash, the most recent writes may be lost.
type FileStore struct {
	dir      string
	clientID string
	config   *fileStoreConfig
}

type fileStoreConfig struct {
	permissions os.FileMode
}

// FileStoreOption configures a FileStore.
type FileStoreOption func(*fileStoreConfig)

// WithPermissions sets the file permissions for stored files.
// Default is 0600 (owner read/write, group/others none).
//
// Example:
//
//	store, _ := mq.NewFileStore("/var/lib/mqtt", "sensor-1",
//	    mq.WithPermissions(0600)) // Owner read/write only
func WithPermissions(perm os.FileMode) FileStoreOption {
	return func(c *fileStoreConfig) {
		c.permissions = perm
	}
}

// NewFileStore creates a file-based session store for the specified client ID.
//
// The baseDir will contain a subdirectory for each client ID, allowing
// multiple clients to share the same base directory.
//
// Example:
//
//	store, err := mq.NewFileStore("/var/lib/mqtt", "sensor-1")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	client, err := mq.Dial("tcp://localhost:1883",
//	    mq.WithClientID("sensor-1"),
//	    mq.WithCleanSession(false),
//	    mq.WithSessionStore(store))
func NewFileStore(baseDir, clientID string, opts ...FileStoreOption) (*FileStore, error) {
	if clientID == "" {
		return nil, fmt.Errorf("clientID cannot be empty")
	}

	if strings.Contains(clientID, "..") || strings.Contains(clientID, string(filepath.Separator)) {
		return nil, fmt.Errorf("clientID contains invalid characters")
	}

	cfg := &fileStoreConfig{
		permissions: 0600,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	dir := filepath.Join(baseDir, clientID)
	if err := os.MkdirAll(dir, cfg.permissions|0111); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	// Create subdirectories for incremental storage
	for _, sub := range []string{"pending", "subscriptions", "qos2"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), cfg.permissions|0111); err != nil {
			return nil, fmt.Errorf("failed to create %s directory: %w", sub, err)
		}
	}

	return &FileStore{
		dir:      dir,
		clientID: clientID,
		config:   cfg,
	}, nil
}

// ClientID returns the client ID this store is bound to.
// This can be used to validate that the store matches the client.
func (f *FileStore) ClientID() string {
	return f.clientID
}

// SavePendingPublish stores a pending publish to disk.
func (f *FileStore) SavePendingPublish(packetID uint16, pub *PersistedPublish) error {
	data, err := json.Marshal(pub)
	if err != nil {
		return fmt.Errorf("failed to marshal publish: %w", err)
	}

	path := filepath.Join(f.dir, "pending", fmt.Sprintf("%d.json", packetID))
	if err := writeFileAtomic(path, data, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write pending publish: %w", err)
	}

	return nil
}

// DeletePendingPublish removes a pending publish from disk.
func (f *FileStore) DeletePendingPublish(packetID uint16) error {
	path := filepath.Join(f.dir, "pending", fmt.Sprintf("%d.json", packetID))
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already deleted
	}
	if err != nil {
		return fmt.Errorf("failed to delete pending publish: %w", err)
	}
	return nil
}

// LoadPendingPublishes loads all pending publishes from disk.
func (f *FileStore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	result := make(map[uint16]*PersistedPublish)

	entries, err := os.ReadDir(filepath.Join(f.dir, "pending"))
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("failed to read pending directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var packetID uint16
		if _, err := fmt.Sscanf(entry.Name(), "%d.json", &packetID); err != nil {
			continue // Skip malformed filenames
		}

		data, err := os.ReadFile(filepath.Join(f.dir, "pending", entry.Name()))
		if err != nil {
			continue // Skip unreadable files
		}

		var pub PersistedPublish
		if err := json.Unmarshal(data, &pub); err != nil {
			continue // Skip corrupted files
		}

		result[packetID] = &pub
	}

	return result, nil
}

// ClearPendingPublishes removes all pending publishes from disk.
func (f *FileStore) ClearPendingPublishes() error {
	return f.resetDir("pending")
}

// SaveSubscription stores a subscription to disk.
func (f *FileStore) SaveSubscription(topic string, sub *PersistedSubscription) error {
	data, err := json.Marshal(sub)
	if err != nil {
		return fmt.Errorf("failed to marshal subscription: %w", err)
	}

	safeTopic := base64.URLEncoding.EncodeToString([]byte(topic))
	path := filepath.Join(f.dir, "subscriptions", safeTopic+".json")
	if err := writeFileAtomic(path, data, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write subscription: %w", err)
	}

	return nil
}

// DeleteSubscription removes a subscription from disk.
func (f *FileStore) DeleteSubscription(topic string) error {
	safeTopic := base64.URLEncoding.EncodeToString([]byte(topic))
	path := filepath.Join(f.dir, "subscriptions", safeTopic+".json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already deleted
	}
	if err != nil {
		return fmt.Errorf("failed to delete subscription: %w", err)
	}
	return nil
}

// LoadSubscriptions loads all subscriptions from disk.
func (f *FileStore) LoadSubscriptions() (map[string]*PersistedSubscription, error) {
	result := make(map[string]*PersistedSubscription)

	entries, err := os.ReadDir(filepath.Join(f.dir, "subscriptions"))
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("failed to read subscriptions directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		encodedTopic := strings.TrimSuffix(entry.Name(), ".json")
		topicBytes, err := base64.URLEncoding.DecodeString(encodedTopic)
		if err != nil {
			continue // Skip malformed filenames
		}
		topic := string(topicBytes)

		data, err := os.ReadFile(filepath.Join(f.dir, "subscriptions", entry.Name()))
		if err != nil {
			continue // Skip unreadable files
		}

		var sub PersistedSubscription
		if err := json.Unmarshal(data, &sub); err != nil {
			continue // Skip corrupted files
		}

		result[topic] = &sub
	}

	return result, nil
}

// SaveReceivedQoS2 marks a QoS 2 packet ID as received.
func (f *FileStore) SaveReceivedQoS2(packetID uint16) error {
	path := filepath.Join(f.dir, "qos2", fmt.Sprintf("%d.json", packetID))
	// Just write an empty file to indicate the ID is received.
	// We don't need any content because the ID is the filename.
	if err := writeFileAtomic(path, nil, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write QoS2 ID: %w", err)
	}
	return nil
}

// DeleteReceivedQoS2 removes a QoS 2 packet ID.
func (f *FileStore) DeleteReceivedQoS2(packetID uint16) error {
	path := filepath.Join(f.dir, "qos2", fmt.Sprintf("%d.json", packetID))
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already deleted
	}
	if err != nil {
		return fmt.Errorf("failed to delete QoS2 ID: %w", err)
	}
	return nil
}

// LoadReceivedQoS2 loads all received QoS 2 packet IDs.
func (f *FileStore) LoadReceivedQoS2() (map[uint16]struct{}, error) {
	result := make(map[uint16]struct{})

	entries, err := os.ReadDir(filepath.Join(f.dir, "qos2"))
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("failed to read qos2 directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var packetID uint16
		if _, err := fmt.Sscanf(entry.Name(), "%d.json", &packetID); err != nil {
			continue // Skip malformed filenames
		}

		result[packetID] = struct{}{}
	}

	return result, nil
}

// ClearReceivedQoS2 removes all received QoS 2 packet IDs.
func (f *FileStore) ClearReceivedQoS2() error {
	return f.resetDir("qos2")
}

// Clear removes all session state from disk.
func (f *FileStore) Clear() error {
	for _, sub := range []string{"pending", "subscriptions", "qos2"} {
		if err := f.resetDir(sub); err != nil {
			return err
		}
	}
	return nil
}

// resetDir empties the sub directory by removing and recreating it, so later
// saves into it keep working.
func (f *FileStore) resetDir(sub string) error {
	path := filepath.Join(f.dir, sub)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to clear %s: %w", sub, err)
	}
	if err := os.MkdirAll(path, f.config.permissions|0111); err != nil {
		return fmt.Errorf("failed to recreate %s: %w", sub, err)
	}
	return nil
}

// writeFileAtomic writes data to path through a temporary file in the same
// directory and a rename, so readers (and a restart after a crash) see either
// the old content or the new one, never a partial file. The temporary name
// starts with ".tmp-", which the Load methods do not accept as an entry.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(perm)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
	}
	return err
}
