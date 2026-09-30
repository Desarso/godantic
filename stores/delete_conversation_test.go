package stores

import (
	"path/filepath"
	"testing"
)

func TestSQLiteDeleteConversation(t *testing.T) {
	store, err := NewSQLiteStoreSimple(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer store.Close()

	if err := store.SaveMessageWithUser("c1", "u1", "user", "user_message", []map[string]string{{"text": "hi"}}, ""); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.SaveMessageWithUser("c2", "u1", "user", "user_message", []map[string]string{{"text": "keep"}}, ""); err != nil {
		t.Fatalf("save: %v", err)
	}

	var deleter ConversationDeleter = store
	if err := deleter.DeleteConversation("c1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if h, _ := store.FetchHistory("c1", 0); len(h) != 0 {
		t.Fatalf("expected c1 history to be gone, got %d messages", len(h))
	}
	if h, _ := store.FetchHistory("c2", 0); len(h) != 1 {
		t.Fatalf("expected c2 to keep 1 message, got %d", len(h))
	}
	convos, _ := store.ListConversationsForUser("u1")
	if len(convos) != 1 || convos[0].ConversationID != "c2" {
		t.Fatalf("expected only c2 to remain, got %+v", convos)
	}
}
