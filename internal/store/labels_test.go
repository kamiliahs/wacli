package store

import (
	"testing"
)

func TestLabelUpsertListAndDelete(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertLabel(Label{ID: "1", Name: "Pendiente", Color: 3}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.UpsertLabel(Label{ID: "1", Name: "En curso", Color: 5}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	got, err := db.GetLabel("1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "En curso" || got.Color != 5 || got.Deleted {
		t.Fatalf("label = %+v", got)
	}

	listed, err := db.ListLabels()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "1" {
		t.Fatalf("list = %+v", listed)
	}

	if err := db.DeleteLabel("1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	listed, err = db.ListLabels()
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("list after delete = %+v", listed)
	}
}

func TestChatLabelAssociations(t *testing.T) {
	db := openTestDB(t)
	chat := "15551112222@s.whatsapp.net"

	if err := db.UpsertLabel(Label{ID: "7", Name: "Procesando", Color: 1}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.SetChatLabel("7", chat, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Applying twice is idempotent.
	if err := db.SetChatLabel("7", chat, true); err != nil {
		t.Fatalf("re-apply: %v", err)
	}

	forChat, err := db.LabelsForChat(chat)
	if err != nil {
		t.Fatalf("for chat: %v", err)
	}
	if len(forChat) != 1 || forChat[0].ID != "7" {
		t.Fatalf("for chat = %+v", forChat)
	}

	chats, err := db.ChatsForLabel("7")
	if err != nil {
		t.Fatalf("chats: %v", err)
	}
	if len(chats) != 1 || chats[0] != chat {
		t.Fatalf("chats = %+v", chats)
	}

	if err := db.SetChatLabel("7", chat, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	forChat, err = db.LabelsForChat(chat)
	if err != nil {
		t.Fatalf("for chat after remove: %v", err)
	}
	if len(forChat) != 0 {
		t.Fatalf("for chat after remove = %+v", forChat)
	}
}

func TestChatLabelKeepsUnknownLabelID(t *testing.T) {
	db := openTestDB(t)
	chat := "15551112222@s.whatsapp.net"

	// The association can arrive before the label definition syncs.
	if err := db.SetChatLabel("99", chat, true); err != nil {
		t.Fatalf("apply unknown: %v", err)
	}
	chats, err := db.ChatsForLabel("99")
	if err != nil {
		t.Fatalf("chats: %v", err)
	}
	if len(chats) != 1 {
		t.Fatalf("chats = %+v", chats)
	}
}
