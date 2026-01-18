package mq

import (
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
//	    pending_1.json
//	    pending_2.json
//	    subscriptions.json
//	    qos2_received.json
//
// This implementation is synchronous - all operations block until complete.
// For async/batched writes, users can implement a custom SessionStore.
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
// Default is 0644 (owner read/write, group/others read-only).
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
		permissions: 0644,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	dir := filepath.Join(baseDir, clientID)
	if err := os.MkdirAll(dir, cfg.permissions|0111); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
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

	path := filepath.Join(f.dir, fmt.Sprintf("pending_%d.json", packetID))
	if err := os.WriteFile(path, data, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write pending publish: %w", err)
	}

	return nil
}

// DeletePendingPublish removes a pending publish from disk.
func (f *FileStore) DeletePendingPublish(packetID uint16) error {
	path := filepath.Join(f.dir, fmt.Sprintf("pending_%d.json", packetID))
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

	files, err := filepath.Glob(filepath.Join(f.dir, "pending_*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list pending publishes: %w", err)
	}

	for _, file := range files {
		var packetID uint16
		base := filepath.Base(file)
		if _, err := fmt.Sscanf(base, "pending_%d.json", &packetID); err != nil {
			continue // Skip malformed filenames
		}

		data, err := os.ReadFile(file)
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
	files, err := filepath.Glob(filepath.Join(f.dir, "pending_*.json"))
	if err != nil {
		return fmt.Errorf("failed to list pending publishes: %w", err)
	}

	for _, file := range files {
		os.Remove(file) // Best effort
	}

	return nil
}

// SaveSubscription stores a subscription to disk.
func (f *FileStore) SaveSubscription(topic string, sub *SubscriptionInfo) error {
	subs, err := f.LoadSubscriptions()
	if err != nil {
		subs = make(map[string]*SubscriptionInfo)
	}

	subs[topic] = sub

	data, err := json.Marshal(subs)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	path := filepath.Join(f.dir, "subscriptions.json")
	if err := os.WriteFile(path, data, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write subscriptions: %w", err)
	}

	return nil
}

// DeleteSubscription removes a subscription from disk.
func (f *FileStore) DeleteSubscription(topic string) error {
	subs, err := f.LoadSubscriptions()
	if err != nil {
		return nil // Nothing to delete
	}

	delete(subs, topic)

	if len(subs) == 0 {
		path := filepath.Join(f.dir, "subscriptions.json")
		os.Remove(path)
		return nil
	}

	data, err := json.Marshal(subs)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	path := filepath.Join(f.dir, "subscriptions.json")
	if err := os.WriteFile(path, data, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write subscriptions: %w", err)
	}

	return nil
}

// LoadSubscriptions loads all subscriptions from disk.
func (f *FileStore) LoadSubscriptions() (map[string]*SubscriptionInfo, error) {
	path := filepath.Join(f.dir, "subscriptions.json")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]*SubscriptionInfo), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read subscriptions: %w", err)
	}

	var subs map[string]*SubscriptionInfo
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subscriptions: %w", err)
	}

	return subs, nil
}

// SaveReceivedQoS2 marks a QoS 2 packet ID as received.
func (f *FileStore) SaveReceivedQoS2(packetID uint16) error {
	qos2, err := f.LoadReceivedQoS2()
	if err != nil {
		qos2 = make(map[uint16]struct{})
	}

	qos2[packetID] = struct{}{}

	ids := make([]uint16, 0, len(qos2))
	for id := range qos2 {
		ids = append(ids, id)
	}

	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("failed to marshal QoS2 IDs: %w", err)
	}

	path := filepath.Join(f.dir, "qos2_received.json")
	if err := os.WriteFile(path, data, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write QoS2 IDs: %w", err)
	}

	return nil
}

// DeleteReceivedQoS2 removes a QoS 2 packet ID.
func (f *FileStore) DeleteReceivedQoS2(packetID uint16) error {
	qos2, err := f.LoadReceivedQoS2()
	if err != nil {
		return nil // Nothing to delete
	}

	delete(qos2, packetID)

	if len(qos2) == 0 {
		path := filepath.Join(f.dir, "qos2_received.json")
		os.Remove(path)
		return nil
	}

	ids := make([]uint16, 0, len(qos2))
	for id := range qos2 {
		ids = append(ids, id)
	}

	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("failed to marshal QoS2 IDs: %w", err)
	}

	path := filepath.Join(f.dir, "qos2_received.json")
	if err := os.WriteFile(path, data, f.config.permissions); err != nil {
		return fmt.Errorf("failed to write QoS2 IDs: %w", err)
	}

	return nil
}

// LoadReceivedQoS2 loads all received QoS 2 packet IDs.
func (f *FileStore) LoadReceivedQoS2() (map[uint16]struct{}, error) {
	path := filepath.Join(f.dir, "qos2_received.json")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[uint16]struct{}), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read QoS2 IDs: %w", err)
	}

	var ids []uint16
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("failed to unmarshal QoS2 IDs: %w", err)
	}

	result := make(map[uint16]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}

	return result, nil
}

// ClearReceivedQoS2 removes all received QoS 2 packet IDs.
func (f *FileStore) ClearReceivedQoS2() error {
	path := filepath.Join(f.dir, "qos2_received.json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Clear removes all session state from disk.
func (f *FileStore) Clear() error {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return fmt.Errorf("failed to read store directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "pending_") ||
			name == "subscriptions.json" ||
			name == "qos2_received.json" {
			_ = os.Remove(filepath.Join(f.dir, name))
		}
	}

	return nil
}
