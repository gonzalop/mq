package mq

import (
	"testing"
	"time"
)

func TestAsyncStore_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	fileStore, err := NewFileStore(tmpDir, "async-test")
	if err != nil {
		t.Fatalf("Failed to create FileStore: %v", err)
	}

	asyncStore := NewAsyncStore(fileStore, 10)
	defer asyncStore.Close()

	// 1. Save something asynchronously
	pub := &PersistedPublish{Topic: "async/topic", Payload: []byte("async data"), QoS: 1}
	if err := asyncStore.SavePendingPublish(1, pub); err != nil {
		t.Fatalf("SavePendingPublish failed: %v", err)
	}

	// 2. Since it's async, it might not be there IMMEDIATELY
	// but Load should be synchronous and might see it if the worker is fast.
	// However, we want to test that it EVENTUALLY gets written.

	deadline := time.Now().Add(1 * time.Second)
	var loaded map[uint16]*PersistedPublish
	for time.Now().Before(deadline) {
		loaded, err = asyncStore.LoadPendingPublishes()
		if err == nil && len(loaded) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(loaded) != 1 {
		t.Fatalf("Data was not persisted asynchronously after 1s")
	}

	// 3. Test Delete
	if err := asyncStore.DeletePendingPublish(1); err != nil {
		t.Fatalf("DeletePendingPublish failed: %v", err)
	}

	for time.Now().Before(deadline.Add(1 * time.Second)) {
		loaded, err = asyncStore.LoadPendingPublishes()
		if err == nil && len(loaded) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(loaded) != 0 {
		t.Fatalf("Data was not deleted asynchronously after 1s")
	}
}

func TestAsyncStore_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	fileStore, _ := NewFileStore(tmpDir, "clear-test")
	asyncStore := NewAsyncStore(fileStore, 10)
	defer asyncStore.Close()

	_ = asyncStore.SaveSubscription("topic/1", &PersistedSubscription{QoS: 1})
	_ = asyncStore.SaveReceivedQoS2(42)

	// Wait for writes
	time.Sleep(50 * time.Millisecond)

	// Clear is synchronous in our implementation
	if err := asyncStore.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	subs, _ := asyncStore.LoadSubscriptions()
	if len(subs) != 0 {
		t.Errorf("Subscriptions not cleared: %d remain", len(subs))
	}

	qos2, _ := asyncStore.LoadReceivedQoS2()
	if len(qos2) != 0 {
		t.Errorf("QoS2 IDs not cleared: %d remain", len(qos2))
	}
}

func TestFileStore_TopicEncoding(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewFileStore(tmpDir, "topic-test")

	// Test topics with special characters
	topics := []string{
		"a/b/c",
		"sensors/+/temp",
		"data/#",
		"special/!@#$%^&*()",
		"unicode/🚀",
	}

	for _, topic := range topics {
		sub := &PersistedSubscription{QoS: 1}
		if err := store.SaveSubscription(topic, sub); err != nil {
			t.Fatalf("Failed to save topic %q: %v", topic, err)
		}
	}

	loaded, err := store.LoadSubscriptions()
	if err != nil {
		t.Fatalf("LoadSubscriptions failed: %v", err)
	}

	if len(loaded) != len(topics) {
		t.Fatalf("Expected %d topics, got %d", len(topics), len(loaded))
	}

	for _, topic := range topics {
		if _, ok := loaded[topic]; !ok {
			t.Errorf("Topic %q was not correctly loaded", topic)
		}
	}
}

type blockingSessionStore struct {
	dummySessionStore
	saveBlock chan struct{}
}

func (b *blockingSessionStore) SavePendingPublish(_ uint16, _ *PersistedPublish) error {
	<-b.saveBlock
	return nil
}

func TestAsyncStore_NonBlocking(t *testing.T) {
	blockChan := make(chan struct{})
	mockStore := &blockingSessionStore{
		saveBlock: blockChan,
	}

	// Create an AsyncStore with initial capacity 2
	asyncStore := NewAsyncStore(mockStore, 2)

	// We will enqueue 10 operations. With the previous buffered channel implementation (bufferSize 2),
	// this would block after enqueuing the 3rd operation because the mock store blocks indefinitely.
	start := time.Now()
	for i := uint16(1); i <= 10; i++ {
		err := asyncStore.SavePendingPublish(i, &PersistedPublish{Topic: "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	duration := time.Since(start)

	if duration > 50*time.Millisecond {
		t.Errorf("AsyncStore blocked! Enqueues took %v, expected < 50ms", duration)
	}

	// Unblock the background worker to allow clean exit
	close(blockChan)
	asyncStore.Close()
}

func TestAsyncStore_LoadFlushesQueue(t *testing.T) {
	saveDone := make(chan struct{})
	mock := &raceCheckingSessionStore{
		saveDone: saveDone,
	}
	asyncStore := NewAsyncStore(mock, 10)
	defer asyncStore.Close()

	_ = asyncStore.SavePendingPublish(1, &PersistedPublish{Topic: "test"})

	_, _ = asyncStore.LoadPendingPublishes()
	if !mock.loadSuccess {
		t.Fatalf("Race condition: Load completed before pending Save completed")
	}
}

type raceCheckingSessionStore struct {
	dummySessionStore
	saveDone    chan struct{}
	loadSuccess bool
}

func (r *raceCheckingSessionStore) SavePendingPublish(_ uint16, _ *PersistedPublish) error {
	time.Sleep(50 * time.Millisecond) // Simulate slow write
	close(r.saveDone)
	return nil
}

func (r *raceCheckingSessionStore) LoadPendingPublishes() (map[uint16]*PersistedPublish, error) {
	select {
	case <-r.saveDone:
		r.loadSuccess = true
	default:
		r.loadSuccess = false
	}
	return nil, nil
}
