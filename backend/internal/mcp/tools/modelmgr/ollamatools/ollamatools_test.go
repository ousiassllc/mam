package ollamatools_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr/ollamatools"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull/ollamatest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

const gemma = "gemma4:12b"

func newEnv(t *testing.T) (*mcptest.Env, *mmtest.Rig, *ollamatest.Rig) {
	t.Helper()
	var mm *mmtest.Rig
	var ol *ollamatest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		mm = mmtest.New(t, mmtest.WithDB(env.DB), mmtest.WithLogger(env.Logger))
		ol = ollamatest.New(t, ollamatest.Shared(mm.Queue, mm.Table))
		mm.Queue.Register(domain.JobModelInstall, ol.Svc.Handler(mm.Svc.HandleInstall), true)
		env.Services.ModelManager, env.Services.Ollama = mm.Svc, ol.Svc
		jobs.Register(r, env.Services)
		modelmgr.Register(r, env.Services)
	})
	return env, mm, ol
}

func lines(t *testing.T, name string) []ollamatest.Line {
	t.Helper()
	for _, m := range catalog.Default().OllamaModels {
		if m.Name == name {
			l := m.ModelLayer()
			return []ollamatest.Line{
				{"status": "pulling manifest"}, ollamatest.Status("pulling x", l.Digest, l.Size, l.Size/2), ollamatest.Status("pulling x", l.Digest, l.Size, l.Size),
				{"status": "verifying sha256 digest"}, {"status": "writing manifest"}, {"status": "success"},
			}
		}
	}
	require.FailNow(t, "no such model")
	return nil
}

// The list has the three models of the catalog in order with the size, the license and the answer for commercial use; it says the server has no authentication; it answers with a stopped server too.
func TestListHasTheThreeModelsTheLicenseAndTheNoAuthNotice(t *testing.T) {
	env, _, _ := newEnv(t)

	var list ollamatools.ListOutput
	env.CallOK("list_ollama_models", map[string]any{}, &list)

	require.Len(t, list.Models, 3)
	assert.Equal(t, []string{"gemma4:12b", "qwen3.8:27b", "deepseek-r1:14b"}, []string{list.Models[0].Name, list.Models[1].Name, list.Models[2].Name})
	assert.Equal(t, "stopped", list.Server)
	assert.True(t, list.NoAuth)
	assert.Contains(t, list.Notice, "認証がありません")
	assert.Empty(t, list.Others)
	for _, m := range list.Models {
		assert.Equal(t, "missing", m.State, m.Name)
		assert.Positive(t, m.SizeBytes, m.Name)
		assert.Equal(t, "yes", string(m.License.Commercial), m.Name)
		assert.NotEmpty(t, m.License.LicenseName, m.Name)
		assert.False(t, m.Verified, m.Name)
	}
	assert.Equal(t, int64(8021618941), list.Models[0].SizeBytes)
}

// A pull is a job: its target is ollama:<name>, get_job reads its progress, and the list then says installed and verified; delete removes it.
func TestPullRunsAsAJobAndDeleteUndoesIt(t *testing.T) {
	env, mm, ol := newEnv(t)
	ol.Fake.Lines = lines(t, gemma)

	var out ollamatools.JobOutput
	env.CallOK("pull_ollama_model", map[string]any{"name": gemma}, &out)

	assert.Equal(t, "ollama:gemma4:12b", out.Target)
	mm.Start()
	var st mcpkit.JobStatus
	require.Eventually(t, func() bool {
		env.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
		return st.State.Final()
	}, 30e9, 20e6)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "ollama:gemma4:12b", st.Result["target"])
	assert.Equal(t, []string{gemma}, ol.Fake.Pulls())
	var list ollamatools.ListOutput
	env.CallOK("list_ollama_models", map[string]any{}, &list)
	assert.Equal(t, "installed", list.Models[0].State)
	assert.True(t, list.Models[0].Verified)

	var del ollamatools.DeletedOutput
	env.CallOK("delete_ollama_model", map[string]any{"name": gemma}, &del)
	assert.True(t, del.Deleted)
	assert.Equal(t, []string{gemma}, ol.Fake.Deletes())
	assert.Contains(t, env.CallError("delete_ollama_model", map[string]any{"name": gemma}), "conflict", "not fetched")
}

// The same refusals as the screen: a name outside the catalog is a validation error before anything is asked of Ollama; no Ollama is a conflict; no room is a conflict; a model an Agent uses is a validation error.
func TestPullAndDeleteRefuseLikeTheScreen(t *testing.T) {
	env, _, ol := newEnv(t)
	for _, bad := range []string{"qwen3.8:27b-cloud", "huihui_ai/qwen3.8:27b", "hf.co/org/repo:Q4_K_M", "llama3.2:3b", ""} {
		assert.Contains(t, env.CallError("pull_ollama_model", map[string]any{"name": bad}), "validation", bad)
		assert.Contains(t, env.CallError("delete_ollama_model", map[string]any{"name": bad}), "validation", bad)
	}
	assert.Empty(t, ol.Fake.Requests())

	ol.SetFree(1 << 30)
	msg := env.CallError("pull_ollama_model", map[string]any{"name": gemma})
	assert.Contains(t, msg, "conflict")
	assert.Contains(t, msg, "空き容量が足りません")
	ol.SetFree(1 << 40)

	ol.Runtime.Installed = false
	assert.Contains(t, env.CallError("pull_ollama_model", map[string]any{"name": gemma}), "conflict")
	var list ollamatools.ListOutput
	env.CallOK("list_ollama_models", map[string]any{}, &list)
	assert.Equal(t, "not_installed", list.Server)
	assert.Equal(t, "unavailable", list.Models[1].State)
	assert.Empty(t, ol.Fake.Requests())
}

// cancel_model_install stops the pull of a model of Ollama too, and the job ends as failed.
func TestCancelModelInstallStopsAPull(t *testing.T) {
	env, mm, ol := newEnv(t)
	ol.Fake.Lines = lines(t, gemma)
	release := ol.Fake.HoldAt(2)
	t.Cleanup(release)
	var out ollamatools.JobOutput
	env.CallOK("pull_ollama_model", map[string]any{"name": gemma}, &out)
	mm.Start()
	ol.Fake.Reached()

	var cancelled modelmgr.CancelOutput
	env.CallOK("cancel_model_install", map[string]any{"job_id": out.JobID}, &cancelled)

	assert.True(t, cancelled.Cancelled)
	var st mcpkit.JobStatus
	require.Eventually(t, func() bool {
		env.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
		return st.State.Final()
	}, 30e9, 20e6)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.False(t, strings.Contains(st.Error, "panic"))
	assert.Contains(t, env.CallError("cancel_model_install", map[string]any{"job_id": out.JobID}), "not_found", "a job that ended")
}

func TestWithoutAnOllamaManagerTheToolsAreUnavailable(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { ollamatools.Register(r, env.Services) })
	assert.Contains(t, env.CallError("list_ollama_models", map[string]any{}), "unavailable")
	for _, tool := range []string{"pull_ollama_model", "delete_ollama_model"} {
		assert.Contains(t, env.CallError(tool, map[string]any{"name": gemma}), "unavailable", tool)
	}
}
