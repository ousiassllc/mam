package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ChatRepository reads a Project's chat thread. Writes go through
// ProjectRepository.Create/Apply so the thread stays consistent with the Project.
type ChatRepository struct{ db *DB }

// NewChatRepository returns a ChatRepository backed by db.
func NewChatRepository(db *DB) *ChatRepository { return &ChatRepository{db: db} }

const messageSelect = `
SELECT id, project_id, role, kind, content, COALESCE(reply_to_message_id, ''), COALESCE(ref_type, ''),
       COALESCE(ref_id, ''), COALESCE(approval_status, ''), attachment_asset_ids, created_at
FROM chat_message`

func scanMessage(row rowScanner) (domain.ChatMessage, error) {
	var (
		m               domain.ChatMessage
		attachments, at string
	)
	if err := row.Scan(&m.ID, &m.ProjectID, &m.Role, &m.Kind, &m.Content, &m.ReplyToMessageID, &m.RefType,
		&m.RefID, &m.ApprovalStatus, &attachments, &at); err != nil {
		return domain.ChatMessage{}, err
	}
	if err := json.Unmarshal([]byte(attachments), &m.AttachmentAssetIDs); err != nil {
		return domain.ChatMessage{}, fmt.Errorf("decode attachment_asset_ids: %w", err)
	}
	t, err := parseTime(at)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	m.CreatedAt = t
	return m, nil
}

// Get returns a message of the Project or ErrNotFound.
func (r *ChatRepository) Get(ctx context.Context, projectID, id string) (domain.ChatMessage, error) {
	m, err := scanMessage(r.db.Reader().QueryRowContext(ctx, messageSelect+` WHERE project_id = ? AND id = ?`, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ChatMessage{}, ErrNotFound
	}
	if err != nil {
		return domain.ChatMessage{}, fmt.Errorf("get message: %w", err)
	}
	return m, nil
}

// List returns the thread in order. With after set, only messages newer than that
// message ID are returned (IDs are time-ordered UUIDv7, so the ID order is the thread order).
func (r *ChatRepository) List(ctx context.Context, projectID, after string) ([]domain.ChatMessage, error) {
	rows, err := r.db.Reader().QueryContext(ctx, messageSelect+` WHERE project_id = ? AND id > ? ORDER BY id`, projectID, after)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var messages []domain.ChatMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// RepliedMessageIDs returns the IDs of the Project's messages that have at least one reply.
func (r *ChatRepository) RepliedMessageIDs(ctx context.Context, projectID string) (map[string]bool, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT DISTINCT reply_to_message_id FROM chat_message WHERE project_id = ? AND reply_to_message_id IS NOT NULL`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list replied messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	replied := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan replied message: %w", err)
		}
		replied[id] = true
	}
	return replied, rows.Err()
}

// LatestOpenQuestion returns the newest Brief question card nobody replied to, or nil.
func (r *ChatRepository) LatestOpenQuestion(ctx context.Context, projectID string) (*domain.ChatMessage, error) {
	m, err := scanMessage(r.db.Reader().QueryRowContext(ctx, messageSelect+` AS m
		WHERE m.project_id = ? AND m.kind = 'question' AND m.ref_type = 'brief_field'
		  AND NOT EXISTS (SELECT 1 FROM chat_message r WHERE r.reply_to_message_id = m.id)
		ORDER BY m.id DESC LIMIT 1`, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get open question: %w", err)
	}
	return &m, nil
}

// ListBriefQuestions returns the Brief question cards asking about the given fields, in thread order.
func (r *ChatRepository) ListBriefQuestions(ctx context.Context, projectID string, fields []string) ([]domain.ChatMessage, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	args := []any{projectID}
	for _, f := range fields {
		args = append(args, f)
	}
	rows, err := r.db.Reader().QueryContext(ctx,
		messageSelect+` WHERE project_id = ? AND kind = 'question' AND ref_type = 'brief_field' AND ref_id IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(fields)), ",")+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list brief questions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var messages []domain.ChatMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// ListByRef returns the messages (of any Project) that point at the given entity.
func (r *ChatRepository) ListByRef(ctx context.Context, refType, refID string) ([]domain.ChatMessage, error) {
	rows, err := r.db.Reader().QueryContext(ctx, messageSelect+` WHERE ref_type = ? AND ref_id = ? ORDER BY id`, refType, refID)
	if err != nil {
		return nil, fmt.Errorf("list messages by ref: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var messages []domain.ChatMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func insertMessage(ctx context.Context, tx *sql.Tx, m domain.ChatMessage, now string) error {
	if m.ID == "" {
		m.ID = domain.NewID()
	}
	attachments := m.AttachmentAssetIDs
	if attachments == nil {
		attachments = []string{}
	}
	raw, err := json.Marshal(attachments)
	if err != nil {
		return fmt.Errorf("marshal attachments: %w", err)
	}
	var approval any
	if m.Kind == domain.KindApproval {
		approval = string(m.ApprovalStatus)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO chat_message (id, project_id, role, kind, content, reply_to_message_id, ref_type, ref_id, approval_status, attachment_asset_ids, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ProjectID, m.Role, m.Kind, m.Content, nullIfEmpty(m.ReplyToMessageID), nullIfEmpty(m.RefType),
		nullIfEmpty(m.RefID), approval, string(raw), now)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// AssetRepository reads Assets. They are written by ProjectRepository.Create/Apply together with the messages that use them.
type AssetRepository struct{ db *DB }

// NewAssetRepository returns an AssetRepository backed by db.
func NewAssetRepository(db *DB) *AssetRepository { return &AssetRepository{db: db} }

func insertAsset(ctx context.Context, tx *sql.Tx, a domain.Asset, now string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ProjectID, a.Type, a.FilePath, a.MimeType, a.SizeBytes, now)
	if err != nil {
		return fmt.Errorf("insert asset: %w", err)
	}
	return nil
}

const assetSelect = `SELECT id, project_id, type, file_path, COALESCE(mime_type, ''), COALESCE(size_bytes, 0), created_at FROM asset`

func scanAsset(row rowScanner) (domain.Asset, error) {
	var (
		a  domain.Asset
		at string
	)
	if err := row.Scan(&a.ID, &a.ProjectID, &a.Type, &a.FilePath, &a.MimeType, &a.SizeBytes, &at); err != nil {
		return domain.Asset{}, err
	}
	t, err := parseTime(at)
	if err != nil {
		return domain.Asset{}, err
	}
	a.CreatedAt = t
	return a, nil
}

// Get returns an Asset of the Project or ErrNotFound.
func (r *AssetRepository) Get(ctx context.Context, projectID, id string) (domain.Asset, error) {
	a, err := scanAsset(r.db.Reader().QueryRowContext(ctx, assetSelect+` WHERE project_id = ? AND id = ?`, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Asset{}, ErrNotFound
	}
	if err != nil {
		return domain.Asset{}, fmt.Errorf("get asset: %w", err)
	}
	return a, nil
}

// ListByIDs returns the Assets of the Project with the given IDs, in the order of ids.
// IDs that do not belong to the Project are simply absent from the result.
func (r *AssetRepository) ListByIDs(ctx context.Context, projectID string, ids []string) ([]domain.Asset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, projectID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := r.db.Reader().QueryContext(ctx,
		assetSelect+` WHERE project_id = ? AND id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[string]domain.Asset, len(ids))
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset: %w", err)
		}
		byID[a.ID] = a
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	assets := make([]domain.Asset, 0, len(ids))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			assets = append(assets, a)
		}
	}
	return assets, nil
}

// LatestOfKind returns the newest message of the Project of the given kind, or nil when there is none.
func (r *ChatRepository) LatestOfKind(ctx context.Context, projectID string, kind domain.MessageKind) (*domain.ChatMessage, error) {
	rows, err := r.db.Reader().QueryContext(ctx, messageSelect+` WHERE project_id = ? AND kind = ? ORDER BY id DESC LIMIT 1`, projectID, kind)
	if err != nil {
		return nil, fmt.Errorf("get latest message: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	m, err := scanMessage(rows)
	if err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}
	return &m, nil
}
