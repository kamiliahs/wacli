package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openclaw/wacli/internal/store"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// LabelChat applies (labeled=true) or removes (labeled=false) a WhatsApp
// Business label on a chat. It mirrors PinChat: same write boundary, same
// local persistence, on the regular app-state collection that carries
// label mutations.
func (a *App) LabelChat(ctx context.Context, jid types.JID, labelID string, labeled bool) error {
	labelID = strings.TrimSpace(labelID)
	if labelID == "" {
		return fmt.Errorf("label id is required")
	}
	release, err := a.beginChatStateWrite(ctx, appstate.WAPatchRegular)
	if err != nil {
		return err
	}
	defer release()
	chatJID := canonicalJIDString(a.canonicalStoreJID(ctx, jid))
	pending, err := a.beginLocalAppStateWrite(appstate.WAPatchRegular)
	if err != nil {
		return err
	}
	postSendEvents, err := a.wa.LabelChat(ctx, jid, labelID, labeled, func() { pending.reserve(a) })
	if err != nil {
		return errors.Join(err, a.failLocalAppStateWrite(ctx, &pending, postSendEvents))
	}
	if !pending.reserved {
		return fmt.Errorf("WhatsApp app state send completed without an apply boundary")
	}
	return a.completeLocalAppStateWrite(ctx, &pending, postSendEvents, func() error {
		return a.db.SetChatLabel(labelID, chatJID, labeled)
	})
}

// SyncLabels pulls the regular collection so label reads see definitions
// and associations created on other devices. Best effort: a failed fetch
// never fails the read, which falls back to local rows. Call it only on a
// connected app; the plain List/LabelsForChat/ChatsForLabel readers below
// stay local so they also work on a read-only store while sync runs.
func (a *App) SyncLabels(ctx context.Context) {
	tracker := &appStatePersistenceTracker{}
	_, _ = a.fetchAndPersistAppState(ctx, appstate.WAPatchRegular, true, tracker)
}

// ListLabels returns known labels from the local store.
func (a *App) ListLabels(ctx context.Context) ([]store.Label, error) {
	return a.db.ListLabels()
}

// LabelsForChat returns the labels applied to a chat from the local store.
// LID resolution needs a WA client, which read-only opens do not have:
// without one the JID is used as given (PN chats need no mapping).
func (a *App) LabelsForChat(ctx context.Context, jid types.JID) ([]store.Label, error) {
	chatJID := canonicalJIDString(jid)
	if a.WA() != nil {
		chatJID = canonicalJIDString(a.canonicalStoreJID(ctx, jid))
	}
	return a.db.LabelsForChat(chatJID)
}

// ChatsForLabel returns the chat JIDs carrying a label from the local store.
func (a *App) ChatsForLabel(ctx context.Context, labelID string) ([]string, error) {
	return a.db.ChatsForLabel(strings.TrimSpace(labelID))
}

// handleLabelEvent persists label definitions and chat associations from
// app-state events, on one-shot commands and on the running sync alike.
func (a *App) handleLabelEvent(ctx context.Context, evt any) error {
	switch v := evt.(type) {
	case *events.LabelEdit:
		if v == nil || v.Action == nil || strings.TrimSpace(v.LabelID) == "" {
			return nil
		}
		if v.Action.GetDeleted() {
			return a.db.DeleteLabel(v.LabelID)
		}
		return a.db.UpsertLabel(store.Label{
			ID:    v.LabelID,
			Name:  v.Action.GetName(),
			Color: v.Action.GetColor(),
		})
	case *events.LabelAssociationChat:
		if v == nil || v.JID.IsEmpty() || v.Action == nil || strings.TrimSpace(v.LabelID) == "" {
			return nil
		}
		chat := a.canonicalStoreJID(ctx, v.JID)
		return a.db.SetChatLabel(v.LabelID, canonicalJIDString(chat), v.Action.GetLabeled())
	}
	return nil
}
