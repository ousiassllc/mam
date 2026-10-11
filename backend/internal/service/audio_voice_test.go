package service_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

// cancellingCloner creates the voice (billed) and then the client goes away.
type cancellingCloner struct{ cancel context.CancelFunc }

func (c cancellingCloner) Clone(context.Context, string, audiodirector.Sample) (string, error) {
	c.cancel()
	return "billed-voice-1", nil
}

func TestCloneVoice_ABilledVoiceIsStoredEvenWhenTheRequestIsCancelled(t *testing.T) {
	dir := t.TempDir()
	db := testdb.Open(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := servicetest.New(t, service.Deps{DB: db, AssetsDir: filepath.Join(dir, "assets"), Audio: audiodirector.Providers{Cloner: cancellingCloner{cancel: cancel}}})
	p, err := svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "商品CM"})
	require.NoError(t, err)

	clone, err := svc.Editor.CloneVoice(ctx, p.ID, "ユイ", "", upload("s.mp3", []byte("sample")))

	require.NoError(t, err)
	assert.Equal(t, "billed-voice-1", clone.VoiceID)
	voices, err := svc.Editor.Voices(context.Background(), p.ID)
	require.NoError(t, err)
	require.Len(t, voices, 1)
	assert.Equal(t, "billed-voice-1", voices[0].VoiceID)
}

// The Audio Director's planner runs on the Reasoning LLM: its open circuit breaker is a 502 with a
// reason, like its timeout, not an "audio mixing failed" with the internal error text.
func TestAudioError_AnOpenLLMCircuitBreakerIsAnAIErrorWithItsReason(t *testing.T) {
	err := service.AudioError(fmt.Errorf("plan: %w", llm.ErrCircuitOpen))

	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Contains(t, ai.Reason, "一時的に利用できません")
	assert.NotContains(t, ai.Reason, "ミキシング")
}
