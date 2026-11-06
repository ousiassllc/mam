// Package realcatalogtest checks the runtimes of the real catalog against a machine that cannot run them. It is a package of its own because the
// directory of the service is at its line budget.
package realcatalogtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) {
	if lrtest.ServeIfAsked() {
		return
	}
	mmtest.Main(m)
}

// The real catalog on a Mac: GPT-SoVITS, Irodori-TTS, llama.cpp, MuseTalk and Ollama are refused, each with its own sentence of the catalog and the cloud
// provider that makes the same thing.
func TestTheRealCatalogRefusesAMacWithTheReasonAndTheCloudFallback(t *testing.T) {
	svc, err := localruntime.New(localruntime.Deps{Root: t.TempDir(), Platform: rtm.Platform{OS: "darwin", Arch: "arm64"}, Queue: mmtest.New(t).Queue})
	require.NoError(t, err)
	list := svc.List(context.Background())
	require.Len(t, list, 5)
	for i, want := range []struct{ id, why, instead string }{
		{"gpt-sovits", "macOS では GPT-SoVITS の自動導入に対応していません", "ElevenLabs"},
		{"irodori-tts", "macOS では Irodori-TTS の自動導入に対応していません", "ElevenLabs"},
		{"llama-cpp", "macOS（Apple Silicon）では llama.cpp の自動導入に対応していません", "Ollama"},
		{"musetalk", "macOS では MuseTalk 1.5 を自動導入できません", "Sync Labs"},
		{"ollama", "macOS（Apple Silicon）では Ollama の自動導入に対応していません", "「別のサーバー」"},
	} {
		assert.Equal(t, want.id, list[i].ID)
		assert.False(t, list[i].Support.Supported)
		assert.Contains(t, list[i].Support.Message(), want.why)
		assert.Contains(t, list[i].Support.Message(), want.instead)
	}
}
