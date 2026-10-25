package modelmgr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

const hfCommit = "0123456789abcdef0123456789abcdef01234567"

var (
	sdxl = hftest.Safetensors([]string{"model.diffusion_model.a", "conditioner.embedders.1.model.b", "first_stage_model.c"}, nil, 2000)
	vae  = hftest.Safetensors([]string{"encoder.x", "decoder.y", "quant_conv.z"}, nil, 200)
)

func hfRepo(nfaa bool) hftest.Repo {
	return hftest.Repo{ID: "o/m", Commit: hfCommit, License: "openrail++", NFAA: nfaa, PipelineTag: "text-to-image", Downloads: 9, Files: []hftest.File{
		{Path: "pony.safetensors", Body: sdxl},
		{Path: "split_files/vae/ae.safetensors", Body: vae},
		{Path: "m.Q4_K_M.gguf", Body: []byte("GGUF....")},
		{Path: "old.ckpt", Body: []byte("pickle..")},
	}}
}

func TestHF_SearchListFilesInstallListDeleteAsToolCalls(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	rig.HF.Add(hfRepo(false))

	var hits modelmgr.SearchHFOutput
	env.CallOK("search_hf_models", map[string]any{"q": "o/m", "kind": "text-to-image"}, &hits)
	require.Len(t, hits.Hits, 1)
	assert.Equal(t, "o/m", hits.Hits[0].Repo)

	var view modelmanager.HFRepoView
	env.CallOK("get_hf_model_files", map[string]any{"repo": "https://huggingface.co/o/m/tree/main"}, &view)
	assert.Equal(t, hfCommit, view.Repo.Commit)
	assert.Equal(t, hfplan.TermsVersion, view.TermsVersion)
	byPath := map[string]hfplan.FileView{}
	for _, f := range view.Files {
		byPath[f.Path] = f
	}
	assert.True(t, byPath["pony.safetensors"].Installable)
	assert.Equal(t, hftest.SHA(sdxl), byPath["pony.safetensors"].SHA256)
	assert.Equal(t, "checkpoints", byPath["pony.safetensors"].Folder)
	assert.False(t, byPath["m.Q4_K_M.gguf"].Installable)
	assert.Contains(t, byPath["old.ckpt"].Reason, "pickle")

	// the terms are the user's to confirm: no record, no job
	args := map[string]any{"repo": "o/m", "commit": hfCommit, "path": "pony.safetensors"}
	assert.Contains(t, env.CallError("install_hf_file", args), "conflict")
	args["accept_terms"], args["terms_version"] = true, hfplan.TermsVersion
	var job modelmgr.JobOutput
	env.CallOK("install_hf_file", args, &job)
	assert.Equal(t, "hf:o/m@"+hfCommit+"/pony.safetensors", job.Target)
	done := jobDone(t, env, rig, job.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, done.State, done.Error)
	assert.Equal(t, job.Target, done.Result["target"])
	assert.Equal(t, "model_install", done.Kind)

	var list modelmgr.UserModelsOutput
	env.CallOK("list_user_models", map[string]any{}, &list)
	require.Len(t, list.Models, 1)
	um := list.Models[0]
	assert.Equal(t, "models/checkpoints/pony.safetensors", um.Path)
	assert.Equal(t, hfCommit, um.Commit)
	assert.Len(t, um.ModelIDs, 2)

	var del modelmgr.UserModelDeleted
	env.CallOK("delete_user_model", map[string]any{"path": um.Path}, &del)
	assert.True(t, del.Deleted)
	assert.Contains(t, env.CallError("delete_user_model", map[string]any{"path": um.Path}), "conflict")
	env.CallOK("list_user_models", map[string]any{}, &list)
	assert.Empty(t, list.Models)
}

func TestHF_RefusalsMapToValidationAndConflict(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	rig.HF.Add(hfRepo(false))
	for _, bad := range []string{"", "nope", "https://hf.co/o/m", "https://huggingface.co/o/m?x=1"} {
		assert.Contains(t, env.CallError("get_hf_model_files", map[string]any{"repo": bad}), "validation", bad)
	}
	assert.Contains(t, env.CallError("search_hf_models", map[string]any{"q": " "}), "validation")
	assert.Contains(t, env.CallError("search_hf_models", map[string]any{"q": "x", "kind": "audio"}), "validation")
	for _, path := range []string{"m.Q4_K_M.gguf", "old.ckpt"} {
		msg := env.CallError("install_hf_file", map[string]any{"repo": "o/m", "commit": hfCommit, "path": path, "accept_terms": true, "terms_version": hfplan.TermsVersion})
		assert.Contains(t, msg, "validation", path)
	}
	msg := env.CallError("install_hf_file", map[string]any{"repo": "o/m", "commit": hfCommit, "path": "pony.safetensors", "folder": "../x", "accept_terms": true, "terms_version": hfplan.TermsVersion})
	assert.Contains(t, msg, "validation")
	assert.Zero(t, rig.HF.Count("/cdn/"), "nothing was downloaded")
}

func TestHF_AnAdultRepositoryIsConflictUntilTheAgeIsConfirmed(t *testing.T) {
	env, rig := newEnv(t)
	rig.InstallEngine()
	rig.HF.Add(hfRepo(true))
	var hits modelmgr.SearchHFOutput
	env.CallOK("search_hf_models", map[string]any{"q": "o/m"}, &hits)
	assert.Empty(t, hits.Hits)
	msg := env.CallError("get_hf_model_files", map[string]any{"repo": "o/m"})
	assert.Contains(t, msg, "conflict")
	assert.Contains(t, msg, "成人向けモデルです")
	msg = env.CallError("install_hf_file", map[string]any{"repo": "o/m", "commit": hfCommit, "path": "pony.safetensors", "accept_terms": true, "terms_version": hfplan.TermsVersion})
	assert.Contains(t, msg, "conflict")
	assert.Zero(t, rig.HF.Count("/cdn/"))

	rig.ConfirmAdult()
	env.CallOK("search_hf_models", map[string]any{"q": "o/m"}, &hits)
	require.Len(t, hits.Hits, 1)
	assert.True(t, hits.Hits[0].NFAA)
	var view modelmanager.HFRepoView
	env.CallOK("get_hf_model_files", map[string]any{"repo": "o/m"}, &view)
	assert.True(t, view.Repo.NFAA)
}

func TestHF_UnavailableWithoutAModelManager(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { modelmgr.Register(r, env.Services) })
	for tool, args := range map[string]map[string]any{
		"search_hf_models": {"q": "x"}, "get_hf_model_files": {"repo": "o/m"}, "list_user_models": {},
		"install_hf_file": {"repo": "o/m", "commit": hfCommit, "path": "x.safetensors"}, "delete_user_model": {"path": "models/vae/x.safetensors"},
	} {
		assert.Contains(t, env.CallError(tool, args), "unavailable", tool)
	}
}

func TestHF_AGatedRepositoryNeedsATokenAndTheAccessBeforeADownload(t *testing.T) {
	env, rig := newEnv(t, mmtest.HFAuth())
	rig.InstallEngine()
	repo := hfRepo(false)
	repo.Gated = true
	rig.HF.Add(repo)
	args := map[string]any{"repo": "o/m", "commit": hfCommit, "path": "pony.safetensors", "accept_terms": true, "terms_version": hfplan.TermsVersion}
	msg := env.CallError("install_hf_file", args)
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, "トークン")

	env.CallOK("set_huggingface_token", map[string]any{"token": rig.HF.Token}, &modelmgr.TokenStatus{})
	msg = env.CallError("install_hf_file", args)
	assert.Contains(t, msg, "アクセスを申請")
	assert.Zero(t, rig.HF.Count("/cdn/"), "nothing was downloaded")

	rig.HF.Granted = true
	var job modelmgr.JobOutput
	env.CallOK("install_hf_file", args, &job)
	assert.Equal(t, mcpkit.JobSucceeded, jobDone(t, env, rig, job.JobID).State)
}
