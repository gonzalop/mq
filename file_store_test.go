package mq

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStore_NewFileStore(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("creates directory structure", func(t *testing.T) {
		store, err := NewFileStore(tmpDir, "test-client")
		if err != nil {
			t.Fatalf("NewFileStore failed: %v", err)
		}

		if store.ClientID() != "test-client" {
			t.Errorf("ClientID() = %q, want %q", store.ClientID(), "test-client")
		}

		expectedDir := filepath.Join(tmpDir, "test-client")
		if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
			t.Errorf("Directory %q was not created", expectedDir)
		}
	})

	t.Run("rejects empty client ID", func(t *testing.T) {
		_, err := NewFileStore(tmpDir, "")
		if err == nil {
			t.Error("Expected error for empty clientID, got nil")
		}
	})

	t.Run("accepts custom permissions", func(t *testing.T) {
		store, err := NewFileStore(tmpDir, "perm-test", WithPermissions(0600))
		if err != nil {
			t.Fatalf("NewFileStore failed: %v", err)
		}

		// Save a publish to test permissions
		pub := &PersistedPublish{
			Topic:   "test/topic",
			Payload: []byte("test"),
			QoS:     1,
		}
		if err := store.SavePendingPublish(1, pub); err != nil {
			t.Fatalf("SavePendingPublish failed: %v", err)
		}

		// Check file permissions
		path := filepath.Join(tmpDir, "perm-test", "pending", "1.json")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("File permissions = %o, want 0600", info.Mode().Perm())
		}
	})
}

func TestFileStore_PendingPublishes(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir, "test-client")
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	t.Run("save and load", func(t *testing.T) {
		pub := &PersistedPublish{
			Topic:   "test/topic",
			Payload: []byte("hello world"),
			QoS:     1,
			Retain:  true,
		}

		if err := store.SavePendingPublish(123, pub); err != nil {
			t.Fatalf("SavePendingPublish failed: %v", err)
		}

		loaded, err := store.LoadPendingPublishes()
		if err != nil {
			t.Fatalf("LoadPendingPublishes failed: %v", err)
		}

		if len(loaded) != 1 {
			t.Fatalf("LoadPendingPublishes returned %d items, want 1", len(loaded))
		}

		loadedPub, ok := loaded[123]
		if !ok {
			t.Fatal("Packet ID 123 not found in loaded publishes")
		}

		if loadedPub.Topic != pub.Topic {
			t.Errorf("Topic = %q, want %q", loadedPub.Topic, pub.Topic)
		}
		if string(loadedPub.Payload) != string(pub.Payload) {
			t.Errorf("Payload = %q, want %q", loadedPub.Payload, pub.Payload)
		}
		if loadedPub.QoS != pub.QoS {
			t.Errorf("QoS = %d, want %d", loadedPub.QoS, pub.QoS)
		}
		if loadedPub.Retain != pub.Retain {
			t.Errorf("Retain = %v, want %v", loadedPub.Retain, pub.Retain)
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := store.DeletePendingPublish(123); err != nil {
			t.Fatalf("DeletePendingPublish failed: %v", err)
		}

		loaded, err := store.LoadPendingPublishes()
		if err != nil {
			t.Fatalf("LoadPendingPublishes failed: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("LoadPendingPublishes returned %d items, want 0", len(loaded))
		}
	})

	t.Run("delete non-existent is safe", func(t *testing.T) {
		if err := store.DeletePendingPublish(999); err != nil {
			t.Errorf("DeletePendingPublish(non-existent) failed: %v", err)
		}
	})

	t.Run("multiple publishes", func(t *testing.T) {
		for i := uint16(1); i <= 5; i++ {
			pub := &PersistedPublish{
				Topic:   "test/topic",
				Payload: []byte{byte(i)},
				QoS:     1,
			}
			if err := store.SavePendingPublish(i, pub); err != nil {
				t.Fatalf("SavePendingPublish(%d) failed: %v", i, err)
			}
		}

		loaded, err := store.LoadPendingPublishes()
		if err != nil {
			t.Fatalf("LoadPendingPublishes failed: %v", err)
		}

		if len(loaded) != 5 {
			t.Errorf("LoadPendingPublishes returned %d items, want 5", len(loaded))
		}
	})

	t.Run("clear", func(t *testing.T) {
		if err := store.Clear(); err != nil {
			t.Fatalf("Clear failed: %v", err)
		}

		loaded, err := store.LoadPendingPublishes()
		if err != nil {
			t.Fatalf("LoadPendingPublishes failed: %v", err)
		}

		if err := store.SavePendingPublish(1, &PersistedPublish{Topic: "after/clear", QoS: 1}); err != nil {
			t.Errorf("SavePendingPublish after clear failed: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("LoadPendingPublishes returned %d items after clear, want 0", len(loaded))
		}
	})
}

func TestFileStore_Subscriptions(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir, "test-client")
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	t.Run("save and load", func(t *testing.T) {
		sub := &PersistedSubscription{
			QoS: 1,
			Options: &PersistedSubscriptionOptions{
				NoLocal:           true,
				RetainAsPublished: false,
				RetainHandling:    2,
			},
		}

		if err := store.SaveSubscription("test/topic", sub); err != nil {
			t.Fatalf("SaveSubscription failed: %v", err)
		}

		loaded, err := store.LoadSubscriptions()
		if err != nil {
			t.Fatalf("LoadSubscriptions failed: %v", err)
		}

		if len(loaded) != 1 {
			t.Fatalf("LoadSubscriptions returned %d items, want 1", len(loaded))
		}

		loadedSub, ok := loaded["test/topic"]
		if !ok {
			t.Fatal("Topic 'test/topic' not found in loaded subscriptions")
		}

		if loadedSub.QoS != sub.QoS {
			t.Errorf("QoS = %d, want %d", loadedSub.QoS, sub.QoS)
		}
		if loadedSub.Options.NoLocal != sub.Options.NoLocal {
			t.Errorf("NoLocal = %v, want %v", loadedSub.Options.NoLocal, sub.Options.NoLocal)
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := store.DeleteSubscription("test/topic"); err != nil {
			t.Fatalf("DeleteSubscription failed: %v", err)
		}

		loaded, err := store.LoadSubscriptions()
		if err != nil {
			t.Fatalf("LoadSubscriptions failed: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("LoadSubscriptions returned %d items, want 0", len(loaded))
		}
	})

	t.Run("multiple subscriptions", func(t *testing.T) {
		topics := []string{"topic/1", "topic/2", "topic/3"}
		for _, topic := range topics {
			sub := &PersistedSubscription{QoS: 1}
			if err := store.SaveSubscription(topic, sub); err != nil {
				t.Fatalf("SaveSubscription(%q) failed: %v", topic, err)
			}
		}

		loaded, err := store.LoadSubscriptions()
		if err != nil {
			t.Fatalf("LoadSubscriptions failed: %v", err)
		}

		if len(loaded) != 3 {
			t.Errorf("LoadSubscriptions returned %d items, want 3", len(loaded))
		}
	})
}

func TestFileStore_ReceivedQoS2(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir, "test-client")
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	t.Run("save and load", func(t *testing.T) {
		if err := store.SaveReceivedQoS2(42); err != nil {
			t.Fatalf("SaveReceivedQoS2 failed: %v", err)
		}

		loaded, err := store.LoadReceivedQoS2()
		if err != nil {
			t.Fatalf("LoadReceivedQoS2 failed: %v", err)
		}

		if len(loaded) != 1 {
			t.Fatalf("LoadReceivedQoS2 returned %d items, want 1", len(loaded))
		}

		if _, ok := loaded[42]; !ok {
			t.Error("Packet ID 42 not found in loaded QoS2 IDs")
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := store.DeleteReceivedQoS2(42); err != nil {
			t.Fatalf("DeleteReceivedQoS2 failed: %v", err)
		}

		loaded, err := store.LoadReceivedQoS2()
		if err != nil {
			t.Fatalf("LoadReceivedQoS2 failed: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("LoadReceivedQoS2 returned %d items, want 0", len(loaded))
		}
	})

	t.Run("multiple IDs", func(t *testing.T) {
		ids := []uint16{1, 2, 3, 4, 5}
		for _, id := range ids {
			if err := store.SaveReceivedQoS2(id); err != nil {
				t.Fatalf("SaveReceivedQoS2(%d) failed: %v", id, err)
			}
		}

		loaded, err := store.LoadReceivedQoS2()
		if err != nil {
			t.Fatalf("LoadReceivedQoS2 failed: %v", err)
		}

		if len(loaded) != 5 {
			t.Errorf("LoadReceivedQoS2 returned %d items, want 5", len(loaded))
		}
	})

	t.Run("clear", func(t *testing.T) {
		if err := store.ClearReceivedQoS2(); err != nil {
			t.Fatalf("ClearReceivedQoS2 failed: %v", err)
		}

		loaded, err := store.LoadReceivedQoS2()
		if err != nil {
			t.Fatalf("LoadReceivedQoS2 failed: %v", err)
		}

		if err := store.SaveReceivedQoS2(1); err != nil {
			t.Errorf("SaveReceivedQoS2 after clear failed: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("LoadReceivedQoS2 returned %d items after clear, want 0", len(loaded))
		}
	})
}

func TestFileStore_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir, "test-client")
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	// Add some data
	pub := &PersistedPublish{Topic: "test", Payload: []byte("data"), QoS: 1}
	if err := store.SavePendingPublish(1, pub); err != nil {
		t.Fatalf("Failed to save pending publish: %v", err)
	}
	if err := store.SaveSubscription("test/topic", &PersistedSubscription{QoS: 1}); err != nil {
		t.Fatalf("Failed to save subscription: %v", err)
	}
	if err := store.SaveReceivedQoS2(42); err != nil {
		t.Fatalf("Failed to save QoS2 ID: %v", err)
	}

	// Clear all
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify everything is gone
	pending, _ := store.LoadPendingPublishes()
	if len(pending) != 0 {
		t.Errorf("Pending publishes not cleared: %d items remain", len(pending))
	}

	subs, _ := store.LoadSubscriptions()
	if len(subs) != 0 {
		t.Errorf("Subscriptions not cleared: %d items remain", len(subs))
	}

	qos2, _ := store.LoadReceivedQoS2()
	if len(qos2) != 0 {
		t.Errorf("QoS2 IDs not cleared: %d items remain", len(qos2))
	}
}

func TestFileStore_LoadEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir, "test-client")
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	t.Run("load pending publishes from empty store", func(t *testing.T) {
		loaded, err := store.LoadPendingPublishes()
		if err != nil {
			t.Fatalf("LoadPendingPublishes failed: %v", err)
		}
		if len(loaded) != 0 {
			t.Errorf("Expected empty map, got %d items", len(loaded))
		}
	})

	t.Run("load subscriptions from empty store", func(t *testing.T) {
		loaded, err := store.LoadSubscriptions()
		if err != nil {
			t.Fatalf("LoadSubscriptions failed: %v", err)
		}
		if len(loaded) != 0 {
			t.Errorf("Expected empty map, got %d items", len(loaded))
		}
	})

	t.Run("load QoS2 from empty store", func(t *testing.T) {
		loaded, err := store.LoadReceivedQoS2()
		if err != nil {
			t.Fatalf("LoadReceivedQoS2 failed: %v", err)
		}
		if len(loaded) != 0 {
			t.Errorf("Expected empty map, got %d items", len(loaded))
		}
	})
}

// TestFileStore_AtomicWrites checks that saves leave no temporary files
// behind, replace existing files, and that a temporary file left by a crash
// is not loaded as an entry.
func TestFileStore_AtomicWrites(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir, "atomic")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if err := store.SavePendingPublish(1, &PersistedPublish{Topic: "old", QoS: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePendingPublish(1, &PersistedPublish{Topic: "new", QoS: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSubscription("a/b", &PersistedSubscription{QoS: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReceivedQoS2(7); err != nil {
		t.Fatal(err)
	}

	for _, sub := range []string{"pending", "subscriptions", "qos2"} {
		tmps, _ := filepath.Glob(filepath.Join(dir, "atomic", sub, ".tmp-*"))
		if len(tmps) != 0 {
			t.Errorf("%s: temporary files left behind: %v", sub, tmps)
		}
		// Simulate a crash between writing the temporary file and the rename.
		leftover := filepath.Join(dir, "atomic", sub, ".tmp-123456")
		if err := os.WriteFile(leftover, []byte(`{"Topic":"ghost","QoS":1}`), 0600); err != nil {
			t.Fatal(err)
		}
	}

	pending, err := store.LoadPendingPublishes()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[1] == nil || pending[1].Topic != "new" {
		t.Errorf("pending = %v, want only packet 1 with topic \"new\"", pending)
	}

	subs, err := store.LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs["a/b"] == nil {
		t.Errorf("subscriptions = %v, want only a/b", subs)
	}

	qos2, err := store.LoadReceivedQoS2()
	if err != nil {
		t.Fatal(err)
	}
	if len(qos2) != 1 {
		t.Errorf("qos2 = %v, want only 7", qos2)
	}
	if _, ok := qos2[7]; !ok {
		t.Errorf("qos2 = %v, want 7", qos2)
	}

	info, err := os.Stat(filepath.Join(dir, "atomic", "pending", "1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("pending file permissions = %o, want 600", perm)
	}
}

// TestFileStore_LongTopicFilter checks that a topic filter too long to be
// base64-encoded into a file name (more than ~190 bytes) is still saved,
// loaded and deleted.
func TestFileStore_LongTopicFilter(t *testing.T) {
	store, err := NewFileStore(t.TempDir(), "long")
	if err != nil {
		t.Fatal(err)
	}
	topic := strings.Repeat("level/", 50) + "#" // 301 bytes

	if err := store.SaveSubscription(topic, &PersistedSubscription{QoS: 2}); err != nil {
		t.Fatalf("SaveSubscription: %v", err)
	}
	subs, err := store.LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if sub := subs[topic]; sub == nil || sub.QoS != 2 || len(subs) != 1 {
		t.Fatalf("LoadSubscriptions = %v, want the long topic with QoS 2", subs)
	}

	if err := store.DeleteSubscription(topic); err != nil {
		t.Fatalf("DeleteSubscription: %v", err)
	}
	if subs, _ := store.LoadSubscriptions(); len(subs) != 0 {
		t.Errorf("after delete: %v, want none", subs)
	}
}

// TestFileStore_LegacySubscriptionFiles checks that subscriptions saved by
// older versions (base64 file name, bare PersistedSubscription) still load,
// are migrated when saved again, and are removed by DeleteSubscription.
func TestFileStore_LegacySubscriptionFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	subsDir := filepath.Join(dir, "legacy", "subscriptions")
	writeLegacy := func(topic string, qos uint8) {
		t.Helper()
		data, _ := json.Marshal(&PersistedSubscription{QoS: qos})
		name := base64.URLEncoding.EncodeToString([]byte(topic)) + ".json"
		if err := os.WriteFile(filepath.Join(subsDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	countFiles := func() int {
		t.Helper()
		entries, err := os.ReadDir(subsDir)
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}

	writeLegacy("old/a", 1)
	writeLegacy("old/b", 2)

	subs, err := store.LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 || subs["old/a"] == nil || subs["old/a"].QoS != 1 || subs["old/b"] == nil || subs["old/b"].QoS != 2 {
		t.Fatalf("LoadSubscriptions = %v, want old/a (QoS 1) and old/b (QoS 2)", subs)
	}

	// Saving again migrates to the new file and drops the old one.
	if err := store.SaveSubscription("old/a", &PersistedSubscription{QoS: 0}); err != nil {
		t.Fatal(err)
	}
	if n := countFiles(); n != 2 {
		t.Errorf("after migrating old/a: %d files, want 2", n)
	}
	subs, _ = store.LoadSubscriptions()
	if subs["old/a"] == nil || subs["old/a"].QoS != 0 {
		t.Errorf("old/a after save = %+v, want QoS 0", subs["old/a"])
	}

	// Deleting removes an old-format file too.
	if err := store.DeleteSubscription("old/b"); err != nil {
		t.Fatal(err)
	}
	subs, _ = store.LoadSubscriptions()
	if _, ok := subs["old/b"]; ok || countFiles() != 1 {
		t.Errorf("old/b still present after delete: %v", subs)
	}
}
