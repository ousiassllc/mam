package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// LatestApprovalCard returns the newest approval card of the Project that points at the
// artifact (ref_type, ref_id), or nil when none was posted.
func (r *ChatRepository) LatestApprovalCard(ctx context.Context, projectID, refType, refID string) (*domain.ChatMessage, error) {
	m, err := scanMessage(r.db.Reader().QueryRowContext(ctx, messageSelect+`
		WHERE project_id = ? AND kind = 'approval' AND ref_type = ? AND ref_id = ? ORDER BY id DESC LIMIT 1`,
		projectID, refType, refID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get approval card: %w", err)
	}
	return &m, nil
}

// LatestUserReply returns the text of the newest user message that replies to the message,
// or "" when nobody replied.
func (r *ChatRepository) LatestUserReply(ctx context.Context, projectID, messageID string) (string, error) {
	var content string
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT content FROM chat_message WHERE project_id = ? AND reply_to_message_id = ? AND role = 'user' ORDER BY id DESC LIMIT 1`,
		projectID, messageID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get reply: %w", err)
	}
	return content, nil
}
