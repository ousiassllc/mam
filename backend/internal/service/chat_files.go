package service

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// splitFiles separates the uploads an offer takes (a LoRA: chatops.FileOffer) from the attachments of the Brief, which keep going to storeUploads.
func splitFiles(rt *chatops.Runtime, uploads []Upload) (rest []Upload, batches []chatops.Batch) {
	files := make([]chatops.File, len(uploads))
	for i, u := range uploads {
		files[i] = chatops.File{Name: u.Name, Size: u.Size, Open: u.Open}
	}
	idx, batches := rt.Split(files)
	for _, i := range idx {
		rest = append(rest, uploads[i])
	}
	return rest, batches
}

// postFiles handles a message that carries files an offer takes (a LoRA): the message is stored first, then each offer checks its files and answers with the card that
// asks before they are used, or the sentence that says why not. Nothing is changed before the card is answered. The message is not a reply to the card it was sent to
// (a reply closes a card): it answers no question, so a Brief question or a confirmation card stays open. An offer that fails is told in the thread, as a failed direction is.
func (s *ChatService) postFiles(ctx context.Context, project domain.Project, user domain.ChatMessage, stored []storedUpload, attached []domain.Asset, batches []chatops.Batch) (Workspace, error) {
	user.ID, user.ReplyToMessageID = domain.NewID(), ""
	if user.Content == "" {
		user.Content = "添付: " + chatops.Names(batches)
	}
	turn := repository.Turn{ProjectID: project.ID, Messages: []domain.ChatMessage{user}, Assets: assetsOf(stored)}
	if err := s.attachToBrief(ctx, &turn, attached); err != nil {
		discardUploads(stored)
		return Workspace{}, err
	}
	if err := s.commit(ctx, turn); err != nil {
		discardUploads(stored)
		return Workspace{}, err
	}
	reply, err := chatops.Answer(chatops.WithAttachments(ctx, attached), project, user.Content, batches)
	if err != nil {
		s.logger.Error("offer the files of a chat message failed", "project_id", project.ID, "message_id", user.ID, "err", err)
		notice := assignIDs([]domain.ChatMessage{assistantText("ファイルを取り込めませんでした: " + commandFailureReason(err))})
		// the request may be the very thing that ended: the notice is recorded anyway
		if cerr := s.commit(context.WithoutCancel(ctx), repository.Turn{ProjectID: project.ID, Messages: notice}); cerr != nil {
			s.logger.Error("post the notice of a failed file offer failed", "project_id", project.ID, "err", cerr)
		}
		return Workspace{}, err
	}
	msgs := assignIDs(reply)
	if err := s.commit(context.WithoutCancel(ctx), repository.Turn{ProjectID: project.ID, Messages: msgs}); err != nil {
		return Workspace{}, err
	}
	return s.workspaceOf(ctx, project.ID, append([]domain.ChatMessage{user}, msgs...))
}
