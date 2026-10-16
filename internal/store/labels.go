package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Label is a WhatsApp Business label synced from app state.
type Label struct {
	ID      string
	Name    string
	Color   int32
	Deleted bool
}

// UpsertLabel inserts or replaces a label definition.
func (d *DB) UpsertLabel(label Label) error {
	if d == nil {
		return fmt.Errorf("nil db")
	}
	id := strings.TrimSpace(label.ID)
	if id == "" {
		return fmt.Errorf("label requires an id")
	}
	if _, err := d.sql.Exec(`
		INSERT INTO labels (id, name, color, deleted, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name,
			color = excluded.color,
			deleted = excluded.deleted,
			updated_at = excluded.updated_at
	`, id, label.Name, int64(label.Color), boolToInt(label.Deleted), time.Now().Unix()); err != nil {
		return fmt.Errorf("upsert label: %w", err)
	}
	return nil
}

// DeleteLabel removes a label definition and all its chat associations.
func (d *DB) DeleteLabel(id string) error {
	if d == nil {
		return fmt.Errorf("nil db")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("label requires an id")
	}
	if _, err := d.sql.Exec(`DELETE FROM label_chats WHERE label_id = ?`, id); err != nil {
		return fmt.Errorf("delete label chats: %w", err)
	}
	if _, err := d.sql.Exec(`DELETE FROM labels WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete label: %w", err)
	}
	return nil
}

// ListLabels returns all known (non-deleted) labels ordered by name.
func (d *DB) ListLabels() ([]Label, error) {
	if d == nil {
		return nil, fmt.Errorf("nil db")
	}
	rows, err := d.sql.Query(`
		SELECT id, name, color FROM labels
		WHERE deleted = 0 ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	defer rows.Close()
	var labels []Label
	for rows.Next() {
		var row Label
		var color int64
		if err := rows.Scan(&row.ID, &row.Name, &color); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		row.Color = int32(color)
		labels = append(labels, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	return labels, nil
}

// SetChatLabel applies (labeled=true) or removes (labeled=false) a label
// on a chat. Unknown label ids are kept as placeholder rows so the
// association survives until the label definition syncs.
func (d *DB) SetChatLabel(labelID, chatJID string, labeled bool) error {
	if d == nil {
		return fmt.Errorf("nil db")
	}
	labelID = strings.TrimSpace(labelID)
	chatJID = strings.TrimSpace(chatJID)
	if labelID == "" || chatJID == "" {
		return fmt.Errorf("label association requires label id and chat jid")
	}
	if !labeled {
		if _, err := d.sql.Exec(
			`DELETE FROM label_chats WHERE label_id = ? AND chat_jid = ?`,
			labelID, chatJID,
		); err != nil {
			return fmt.Errorf("remove chat label: %w", err)
		}
		return nil
	}
	if _, err := d.sql.Exec(
		`INSERT INTO labels (id) VALUES (?) ON CONFLICT (id) DO NOTHING`,
		labelID,
	); err != nil {
		return fmt.Errorf("ensure label row: %w", err)
	}
	if _, err := d.sql.Exec(
		`INSERT INTO label_chats (label_id, chat_jid) VALUES (?, ?)
		ON CONFLICT (label_id, chat_jid) DO NOTHING`,
		labelID, chatJID,
	); err != nil {
		return fmt.Errorf("apply chat label: %w", err)
	}
	return nil
}

// LabelsForChat returns the labels applied to a chat.
func (d *DB) LabelsForChat(chatJID string) ([]Label, error) {
	if d == nil {
		return nil, fmt.Errorf("nil db")
	}
	rows, err := d.sql.Query(`
		SELECT l.id, l.name, l.color FROM labels l
		INNER JOIN label_chats lc ON lc.label_id = l.id
		WHERE lc.chat_jid = ? AND l.deleted = 0 ORDER BY l.name
	`, strings.TrimSpace(chatJID))
	if err != nil {
		return nil, fmt.Errorf("labels for chat: %w", err)
	}
	defer rows.Close()
	var labels []Label
	for rows.Next() {
		var row Label
		var color int64
		if err := rows.Scan(&row.ID, &row.Name, &color); err != nil {
			return nil, fmt.Errorf("scan chat label: %w", err)
		}
		row.Color = int32(color)
		labels = append(labels, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("labels for chat: %w", err)
	}
	return labels, nil
}

// ChatsForLabel returns the chat JIDs carrying a label.
func (d *DB) ChatsForLabel(labelID string) ([]string, error) {
	if d == nil {
		return nil, fmt.Errorf("nil db")
	}
	rows, err := d.sql.Query(`
		SELECT chat_jid FROM label_chats WHERE label_id = ? ORDER BY chat_jid
	`, strings.TrimSpace(labelID))
	if err != nil {
		return nil, fmt.Errorf("chats for label: %w", err)
	}
	defer rows.Close()
	var chats []string
	for rows.Next() {
		var jid string
		if err := rows.Scan(&jid); err != nil {
			return nil, fmt.Errorf("scan labeled chat: %w", err)
		}
		chats = append(chats, jid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("chats for label: %w", err)
	}
	return chats, nil
}

var errNoLabelRow = errors.New("sql: no rows in result set")

// GetLabel returns one label definition, if stored.
func (d *DB) GetLabel(id string) (Label, error) {
	if d == nil {
		return Label{}, fmt.Errorf("nil db")
	}
	var row Label
	var color, deleted int64
	err := d.sql.QueryRow(
		`SELECT id, name, color, deleted FROM labels WHERE id = ?`,
		strings.TrimSpace(id),
	).Scan(&row.ID, &row.Name, &color, &deleted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Label{}, errNoLabelRow
		}
		return Label{}, fmt.Errorf("get label: %w", err)
	}
	row.Color = int32(color)
	row.Deleted = deleted != 0
	return row, nil
}
