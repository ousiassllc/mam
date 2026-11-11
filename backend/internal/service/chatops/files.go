package chatops

import (
	"context"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Batch is the files of a message that one offer takes.
type Batch struct {
	Offer FileOffer
	Files []File
}

// Split separates the files an offer takes from the others: rest holds the indexes of the others (they stay the attachments of the Brief), in order; the files of an
// offer come together, in order, in one batch.
func (r *Runtime) Split(files []File) (rest []int, batches []Batch) {
	for i, f := range files {
		offer, ok := r.OfferFor(f.Name)
		if !ok {
			rest = append(rest, i)
			continue
		}
		at := -1
		for j, b := range batches {
			if b.Offer == offer {
				at = j
			}
		}
		if at < 0 {
			batches, at = append(batches, Batch{Offer: offer}), len(batches)
		}
		batches[at].Files = append(batches[at].Files, f)
	}
	return rest, batches
}

// Names are the names of the files of the batches, joined as the text of a message that came with nothing else.
func Names(batches []Batch) string {
	var names []string
	for _, b := range batches {
		for _, f := range b.Files {
			names = append(names, f.Name)
		}
	}
	return strings.Join(names, "、")
}

// Answer asks each offer about its files (content is the text of the message). A refusal becomes the sentence that says why the files were not taken; any other error is
// returned.
func Answer(ctx context.Context, project domain.Project, content string, batches []Batch) ([]domain.ChatMessage, error) {
	var reply []domain.ChatMessage
	for _, b := range batches {
		msgs, err := b.Offer.Offer(ctx, project, content, b.Files)
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			msgs = []domain.ChatMessage{{Role: domain.RoleAssistant, Kind: domain.KindText, Content: "取り込めませんでした: " + ref.Message}}
		case err != nil:
			return nil, err
		}
		reply = append(reply, msgs...)
	}
	return reply, nil
}
