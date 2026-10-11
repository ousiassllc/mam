package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestNotes_APanickingStateExtractorDoesNotEndTheProcess(t *testing.T) {
	f := newGenFixture(t, true)
	proposed := make(chan struct{}, 1)
	f.llm.extractor = func(user string) string {
		if !strings.Contains(user, "## Current world state") {
			return `{"category":"character"}`
		}
		proposed <- struct{}{}
		panic("extractor boom")
	}
	f.buildReferencePack(t)

	_, err := f.svc.Notes.CreateProjectNote(context.Background(), f.projectID, service.NoteInput{Title: "Mio", Content: "黒髪ロングで落ち着いた性格"})
	require.NoError(t, err)
	select {
	case <-proposed:
	case <-time.After(10 * time.Second):
		t.Fatal("the State Extractor was never asked to propose")
	}
	time.Sleep(100 * time.Millisecond) // an unrecovered panic in the background goroutine would end the test binary
	assert.Empty(t, f.promotionCards(t))
}
