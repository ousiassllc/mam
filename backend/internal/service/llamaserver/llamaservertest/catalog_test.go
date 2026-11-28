package llamaservertest_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/llamaserver"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// settingsWithCatalog is the settings service whose llama.cpp offers its whole list to the picker of the LLM assignment (#1235), as the app wires it.
func settingsWithCatalog(t *testing.T, w *wiring) *llmlocal.Service {
	t.Helper()
	settings := llmlocal.New(filepath.Join(t.TempDir(), "llm.json"), false, credentialtest.New(), nil)
	server := w.server
	server.Catalog = llamaserver.LlamaCatalog(w.r.svc)
	settings.SetManaged(server)
	return settings
}

func uncensoredIDs(models []llmlocal.ManagedModel) []string {
	var out []string
	for _, m := range models {
		if m.Uncensored {
			out = append(out, m.ID)
		}
	}
	return out
}

// On a PC that cannot run llama.cpp (the reported case: not Windows + NVIDIA) every model of the list is still offered to the picker, the four uncensored ones among them, each with the
// reason of the PC rather than missing from the list.
func TestOnAPCThatCannotRunItEveryModelIsListedWithTheReason(t *testing.T) {
	mac := newRig(t, platform("darwin", "arm64"))
	settings := settingsWithCatalog(t, wire(t, mac, nil))

	got := settings.ManagedCatalog(ctx())

	require.Len(t, got, 8)
	assert.Equal(t, []string{"gemma-4-12b-it-abliterated", "qwen3.8-27b-abliterated", "qwen3.8-27b-abliterated-q3km", "deepseek-r1-14b-abliterated-v2"}, uncensoredIDs(got))
	for _, m := range got {
		assert.Contains(t, m.Reason, "macOS（Apple Silicon）では llama.cpp の自動導入に対応していません", m.ID)
		assert.Equal(t, "この PC の llama.cpp", m.Server)
	}
}

// Installed, with the weights of the model in force: that one can be chosen (when the calls go to llama.cpp); the others say which model is in force and where to change it, and
// every model has its license, the DeepSeek abliterated-v2 with an answer of "unknown".
func TestInstalledOnlyTheModelInForceIsUsableAndTheOthersSayWhichIs(t *testing.T) {
	r := newRig(t)
	r.install()
	w := wire(t, r, nil)
	w.useManaged(true)
	settings := settingsWithCatalog(t, w)
	on := true
	require.NoError(t, settings.Save(llmlocal.SaveInput{UseManaged: &on}))

	got := settings.ManagedCatalog(ctx())

	require.Len(t, got, 8)
	for _, m := range got {
		if m.ID == "gemma-4-12b-it" {
			assert.Empty(t, m.Reason, "the model in force")
			assert.Equal(t, modellicense.Yes, m.License.Commercial)
			continue
		}
		assert.Contains(t, m.Reason, "今の llama.cpp のモデルは「Gemma 4 12B-it", m.ID)
		assert.Contains(t, m.Reason, "モデルマネージャーの「ローカル LLM」", m.ID)
		assert.NotEmpty(t, m.License.LicenseName, m.ID)
		if m.ID == "deepseek-r1-14b-abliterated-v2" {
			assert.Equal(t, modellicense.Unknown, m.License.Commercial, "no license on the card: never said to be commercial")
			assert.NotEmpty(t, m.License.Notice)
		}
	}

	// the calls go to an address of the settings: the model in force is usable only after "使うサーバー" is changed.
	off := false
	require.NoError(t, settings.Save(llmlocal.SaveInput{UseManaged: &off}))
	for _, m := range settings.ManagedCatalog(ctx()) {
		if m.ID == "gemma-4-12b-it" {
			assert.Contains(t, m.Reason, "「使うサーバー」は「別のサーバー」です", m.ID)
		}
	}
}
