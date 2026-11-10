package chatops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// offerOf takes the files with the extension ext and answers with the result it was built with.
type offerOf struct {
	ext string
	err error
	got []chatops.File
}

func (o *offerOf) Accepts(name string) bool { return strings.HasSuffix(name, o.ext) }

func (o *offerOf) Offer(_ context.Context, _ domain.Project, message string, files []chatops.File) ([]domain.ChatMessage, error) {
	o.got = files
	if o.err != nil {
		return nil, o.err
	}
	return []domain.ChatMessage{{Content: o.ext + ":" + message}}, nil
}

func TestSplit_FilesOfAnOfferComeTogetherAndTheOthersStayInOrder(t *testing.T) {
	rt := chatops.NewRuntime()
	lora, other := &offerOf{ext: ".safetensors"}, &offerOf{ext: ".ckpt"}
	rt.Files(lora)
	rt.Files(other)

	rest, batches := rt.Split([]chatops.File{{Name: "a.png"}, {Name: "x.safetensors"}, {Name: "b.mp4"}, {Name: "y.ckpt"}, {Name: "z.safetensors"}})

	assert.Equal(t, []int{0, 2}, rest)
	require.Len(t, batches, 2)
	assert.Same(t, lora, batches[0].Offer)
	assert.Equal(t, []string{"x.safetensors", "z.safetensors"}, []string{batches[0].Files[0].Name, batches[0].Files[1].Name})
	assert.Same(t, other, batches[1].Offer)
	assert.Equal(t, "x.safetensors、z.safetensors、y.ckpt", chatops.Names(batches), "in the order of the batches")
}

func TestSplit_WithoutAnOfferEveryFileStays(t *testing.T) {
	rest, batches := chatops.NewRuntime().Split([]chatops.File{{Name: "a.png"}, {Name: "x.safetensors"}})
	assert.Equal(t, []int{0, 1}, rest)
	assert.Empty(t, batches)
}

func TestAnswer_ARefusalIsASentenceAndAnotherErrorIsReturned(t *testing.T) {
	ok, refusing, failing := &offerOf{ext: ".a"}, &offerOf{ext: ".b", err: chatops.Refuse("大きすぎます")}, &offerOf{ext: ".c", err: errors.New("disk")}
	files := []chatops.File{{Name: "f"}}

	msgs, err := chatops.Answer(context.Background(), domain.Project{}, "これ", []chatops.Batch{{Offer: ok, Files: files}, {Offer: refusing, Files: files}})
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, ".a:これ", msgs[0].Content)
	assert.Equal(t, domain.RoleAssistant, msgs[1].Role)
	assert.Equal(t, "取り込めませんでした: 大きすぎます", msgs[1].Content)

	_, err = chatops.Answer(context.Background(), domain.Project{}, "", []chatops.Batch{{Offer: failing, Files: files}})
	assert.EqualError(t, err, "disk")
}

func TestIsDecline_TheWordsThatDeclineApartFromBlanksAndMarks(t *testing.T) {
	for _, yes := range []string{"やめる", " やめて。", "No!", "キャンセル"} {
		assert.True(t, chatops.IsDecline(yes), yes)
	}
	for _, no := range []string{"はい", "やめるのをやめる", ""} {
		assert.False(t, chatops.IsDecline(no), no)
	}
}
