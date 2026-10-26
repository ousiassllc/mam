package integration_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// loraD is the LoRA of the adult model m-d, a mirror of a third party; its base file is the checkpoint of m-a.
var loraD = file{"loras", "d.safetensors", []byte(strings.Repeat("D", 50_000))}

const mirrorNote = "第三者のミラー。原本は Civitai（model 1）で、SHA-256 が一致することを 2026-10-25 に確かめた。"

// withAdultModel adds the adult model m-d (the LoRA is served by the fake Hugging Face) to the catalog of the rig.
func withAdultModel() option {
	return func(d *modelmanager.Deps, r *rig) {
		r.host.mu.Lock()
		r.host.files["/"+loraD.folder+"/"+loraD.name] = loraD
		r.host.mu.Unlock()
		cf := func(f file, note string) catalog.File {
			return catalog.File{Name: f.name, Folder: f.folder, URL: r.host.URL + "/" + f.folder + "/" + f.name, SHA256: f.sha(), Size: int64(len(f.body)), SourceNote: note}
		}
		d.Catalog.Models = append(d.Catalog.Models, catalog.Model{
			ID: "m-d", Name: "D", Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, ContentRating: catalog.RatingAdult, Description: "d",
			TriggerWords: []string{"nsfwsks"}, Files: []catalog.File{cf(ckptA, ""), cf(loraD, mirrorNote)},
			VRAM:    catalog.VRAM{MinGB: 8, Source: "test", Estimate: true},
			License: catalog.License{ID: "unspecified", Version: "1", Name: "unspecified", URL: "https://example.com/u", Summary: "s"}, ConsentRequired: true,
			Workflows: []catalog.Workflow{{Name: "wf-d", Template: "txt2img-flux-dev-lora"}},
		})
	}
}

func (r *rig) confirmAdult() {
	r.t.Helper()
	require.NoError(r.t, r.svc.SetAdultContent(true, confirmation()))
}

func (r *rig) installAccepting(id string) modelmanager.JobStatus {
	r.t.Helper()
	m := r.mustModel(id)
	ref, err := r.svc.InstallModel(ctx(), id, &modelmanager.LicenseRef{ID: m.License.ID, Version: m.License.Version})
	require.NoError(r.t, err)
	return r.wait(ref.JobID)
}

func ids(views []modelmanager.ModelView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.ID)
	}
	return out
}

func confirmation() *modelmanager.AdultConfirmation {
	return &modelmanager.AdultConfirmation{Adult: true, NoticeVersion: modelmanager.AdultNoticeVersion}
}

// The age gate is enforced by the Service: with no confirmation on record an adult model is not listed and the install and the
// acceptance of its license are refused before anything is downloaded, whatever the caller is (screen, MCP, another feature).
func TestAdultModelIsHiddenAndRefusedUntilTheAgeIsConfirmed(t *testing.T) {
	r := newRig(t, withAdultModel())
	r.installEngine()
	assert.False(t, r.svc.AdultContentEnabled())
	assert.Equal(t, []string{"m-a", "m-b", "m-c"}, ids(r.svc.Catalog()), "off by default: no adult model in the list")
	_, err := r.svc.Model("m-d")
	require.ErrorIs(t, err, modelmanager.ErrAdultNotConfirmed)
	_, err = r.svc.Model("nope")
	require.ErrorIs(t, err, modelmanager.ErrNotFound)

	before := len(r.host.requests())
	lic := &modelmanager.LicenseRef{ID: "unspecified", Version: "1"}
	_, err = r.svc.InstallModel(ctx(), "m-d", lic)
	require.ErrorIs(t, err, modelmanager.ErrAdultNotConfirmed, "even with the license accepted in the same call")
	require.ErrorIs(t, r.svc.AcceptLicense("m-d", *lic), modelmanager.ErrAdultNotConfirmed)
	assert.Equal(t, before, len(r.host.requests()), "nothing was downloaded")
	assert.NotContains(t, readState(t, r), "adult:m-d", "no consent was recorded")
	assert.True(t, r.svc.HidesModel("comfyui:bundled-comfyui/wf-d"))
	assert.False(t, r.svc.HidesModel("comfyui:bundled-comfyui/wf-a"), "a general model is never hidden")
	assert.False(t, r.svc.HidesModel("comfyui:gpu-box/wf-d"), "only the models of the bundled endpoint")
	assert.False(t, r.svc.HidesModel("imagen-4"))
}

func readState(t *testing.T, r *rig) string {
	t.Helper()
	data, err := os.ReadFile(r.layout.StatePath())
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

// Turning the setting on records the confirmation with its time and the version of the wording in state.json; it is refused
// without the confirmation of the age and of the wording that is current. Turning it off keeps the record.
func TestAdultContentNeedsTheConfirmationAndIsRecorded(t *testing.T) {
	r := newRig(t, withAdultModel())
	st := r.svc.AdultContent()
	assert.False(t, st.Enabled)
	assert.False(t, st.Confirmed)
	assert.Equal(t, modelmanager.AdultNoticeVersion, st.NoticeVersion)
	assert.Contains(t, st.Notice, "未成年（未成年に見える表現を含む）")
	assert.Contains(t, st.Notice, "実在人物の同意のない性的・なりすまし画像")
	assert.Contains(t, st.Notice, "Qwen 系は Research License で商用不可、Flux.1 Dev は非商用")

	require.ErrorIs(t, r.svc.SetAdultContent(true, nil), modelmanager.ErrAdultNotConfirmed, "never confirmed: no way to turn it on without asking")
	var invalid *modelmanager.InvalidError
	require.ErrorAs(t, r.svc.SetAdultContent(true, &modelmanager.AdultConfirmation{Adult: false, NoticeVersion: modelmanager.AdultNoticeVersion}), &invalid)
	require.ErrorAs(t, r.svc.SetAdultContent(true, &modelmanager.AdultConfirmation{Adult: true, NoticeVersion: "0"}), &invalid, "the wording of another version")
	require.ErrorAs(t, r.svc.SetAdultContent(true, &modelmanager.AdultConfirmation{Adult: true}), &invalid)
	assert.False(t, r.svc.AdultContentEnabled())
	assert.NoError(t, r.svc.SetAdultContent(false, nil), "turning it off needs nothing, and records nothing")
	assert.NotContains(t, readState(t, r), "adult_content")

	start := time.Now()
	require.NoError(t, r.svc.SetAdultContent(true, confirmation()))
	st = r.svc.AdultContent()
	assert.True(t, st.Enabled)
	assert.True(t, st.Confirmed)
	require.NotNil(t, st.ConfirmedAt)
	confirmedAt := *st.ConfirmedAt
	assert.WithinDuration(t, start, confirmedAt, time.Minute)
	var rec struct {
		AdultContent struct {
			Enabled       bool      `json:"enabled"`
			NoticeVersion string    `json:"notice_version"`
			ConfirmedAt   time.Time `json:"confirmed_at"`
		} `json:"adult_content"`
	}
	require.NoError(t, json.Unmarshal([]byte(readState(t, r)), &rec))
	assert.True(t, rec.AdultContent.Enabled)
	assert.Equal(t, modelmanager.AdultNoticeVersion, rec.AdultContent.NoticeVersion)
	assert.True(t, confirmedAt.Equal(rec.AdultContent.ConfirmedAt), "the time of the confirmation is in state.json")

	require.NoError(t, r.svc.SetAdultContent(false, nil))
	st = r.svc.AdultContent()
	assert.False(t, st.Enabled)
	assert.True(t, st.Confirmed, "off keeps the confirmation: it is asked once")
	assert.Equal(t, []string{"m-a", "m-b", "m-c"}, ids(r.svc.Catalog()))
	require.NoError(t, r.svc.SetAdultContent(true, nil), "on again needs no new confirmation")
	assert.True(t, r.svc.AdultContent().ConfirmedAt.Equal(confirmedAt), "and does not rewrite it")
	assert.Equal(t, []string{"m-a", "m-b", "m-c", "m-d"}, ids(r.svc.Catalog()))
}

// With the gate open an adult model installs like any other (its own license consent is separate and still required); its
// base file is shared with another model, so it is fetched once and kept while one of the two is installed; the LoRA is a
// mirror whose original is shown.
func TestAdultModelInstallsWithItsOwnConsentAndSharesItsBaseFile(t *testing.T) {
	r := newRig(t, withAdultModel())
	r.installEngine()
	r.confirmAdult()
	d := r.mustModel("m-d")
	assert.Equal(t, "adult", string(d.ContentRating))
	assert.Equal(t, "advanced", string(d.Tier))
	assert.True(t, d.ConsentRequired)
	assert.Equal(t, []string{"nsfwsks"}, d.TriggerWords)
	assert.Equal(t, modelmanager.AdultNotice, d.AdultNotice)
	require.Len(t, d.Files, 2)
	assert.Empty(t, d.Files[0].SourceNote)
	assert.Equal(t, mirrorNote, d.Files[1].SourceNote)

	_, err := r.svc.InstallModel(ctx(), "m-d", nil)
	require.ErrorIs(t, err, modelmanager.ErrConsentRequired, "the age confirmation does not replace the license consent")
	assert.False(t, r.mustModel("m-d").Accepted)

	require.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
	requestsA := len(r.host.requests())
	require.Equal(t, domain.JobSucceeded, r.installAccepting("m-d").State) // accepts the license, then installs
	assert.Equal(t, requestsA+1, len(r.host.requests()), "the checkpoint of m-a is not fetched again: only the LoRA is")
	assert.True(t, r.mustModel("m-d").Accepted)
	assert.Equal(t, []string{"m-d"}, r.mustModel("m-a").Files[0].SharedWith)
	assert.FileExists(t, r.path(loraD))

	require.NoError(t, r.svc.DeleteModel(ctx(), "m-a"))
	assert.FileExists(t, r.path(ckptA), "the base stays while the LoRA model is installed")
	require.NoError(t, r.svc.DeleteModel(ctx(), "m-d"))
	assert.NoFileExists(t, r.path(ckptA))
	assert.NoFileExists(t, r.path(loraD))
}

// A close of the gate while an install waits in the queue stops it: the job refuses at its start, downloads nothing.
func TestClosingTheGateStopsAQueuedAdultInstall(t *testing.T) {
	r := newRig(t, withAdultModel())
	r.installEngine()
	r.confirmAdult()
	ref, err := r.svc.InstallModel(ctx(), "m-d", &modelmanager.LicenseRef{ID: "unspecified", Version: "1"})
	require.NoError(t, err)
	requests := len(r.host.requests())
	require.NoError(t, r.svc.SetAdultContent(false, nil))

	st := r.wait(ref.JobID)
	assert.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "成人向けモデルです")
	assert.Equal(t, requests, len(r.host.requests()))
	assert.NoFileExists(t, r.path(loraD))
}

// A mirror that was deleted is a distinct failure: the message says the source is gone and where the original is.
func TestDeletedMirrorIsReportedAsSourceGone(t *testing.T) {
	r := newRig(t, withAdultModel())
	r.installEngine()
	r.confirmAdult()
	r.host.gone(loraD)

	st := r.installAccepting("m-d")
	assert.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "取得元が消えています")
	assert.Contains(t, st.Error, loraD.name)
	assert.Contains(t, st.Error, mirrorNote, "where the original is")
	assert.NotEqual(t, modelmanager.ModelInstalled, r.mustModel("m-d").State)

	// the same 404 on a file of an original repository is also "gone" (without an origin to name), not a generic failure
	r.host.gone(unetB)
	st = r.installAccepting("m-b")
	assert.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "取得元が消えています")
}

// gone makes the fake host answer 404 for the file from now on: the mirror of a third party was deleted.
func (h *fakeHost) gone(f file) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.files, "/"+f.folder+"/"+f.name)
}
