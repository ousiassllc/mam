package loraimport_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile/lorafiletest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

// A valid file is copied into the managed loras folder under its name, with what the user confirmed recorded; the file is the same bytes and nothing
// is left in the imports folder. The metadata of the file only fills the form: what is recorded is what the user confirmed.
func TestImport_AValidLoRAIsPlacedInTheLorasFolderAndRecorded(t *testing.T) {
	r := newRig(t, nil)
	body := lorafiletest.LoRA(map[string]string{"ss_base_model_version": "sdxl_base_v1-0", "modelspec.trigger_phrase": "mychar"})
	st := r.mustStage("My Char.safetensors", body)
	assert.Equal(t, "My_Char.safetensors", st.Name)
	assert.True(t, st.Renamed)
	assert.Equal(t, sum(body), st.SHA256)
	assert.Equal(t, []string{"sdxl"}, st.Meta.BaseModels)
	assert.Nil(t, st.Conflict)
	assert.Empty(t, r.files(), "nothing is in the loras folder until the user confirms")

	strength := 0.7
	m, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token, Meta: loraimport.Meta{BaseModels: []string{"sdxl"}, Strength: &strength, TriggerWords: "mychar, red dress"}})
	require.NoError(t, err)
	assert.Equal(t, "models/loras/My_Char.safetensors", m.Path)

	got, err := os.ReadFile(filepath.Join(r.lorasDir(), "My_Char.safetensors"))
	require.NoError(t, err)
	assert.Equal(t, body, got)
	assert.Empty(t, r.leftovers(), "the staged file was moved, not copied")
	rec := r.state.Get().LocalLoRAs["models/loras/My_Char.safetensors"]
	assert.Equal(t, []string{"sdxl"}, rec.BaseModels)
	assert.Equal(t, []string{"mychar", "red dress"}, rec.TriggerWords)
	require.NotNil(t, rec.Strength)
	assert.InDelta(t, 0.7, *rec.Strength, 1e-9)
	assert.Equal(t, sum(body), rec.SHA256)
	assert.Empty(t, rec.LicenseName, "no license entered: unknown, never commercial use OK")
	assert.Empty(t, rec.Commercial)
	list := r.svc.List()
	require.Len(t, list, 1)
	assert.True(t, list[0].Present)
}

// What the user did not confirm is not recorded: metadata the file carries is not taken in behind their back, and an empty form means "not set".
func TestCommit_AnEmptyFormRecordsNothingThatTheFileSaid(t *testing.T) {
	r := newRig(t, nil)
	st := r.mustStage("a.safetensors", lorafiletest.LoRA(map[string]string{"ss_base_model_version": "sdxl_base_v1-0", "modelspec.trigger_phrase": "x", "modelspec.license": "MIT"}))
	_, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token})
	require.NoError(t, err)
	rec := r.state.Get().LocalLoRAs["models/loras/a.safetensors"]
	assert.Empty(t, rec.BaseModels)
	assert.Empty(t, rec.TriggerWords)
	assert.Nil(t, rec.Strength)
	assert.Empty(t, rec.LicenseName)
}

// A file that is not a LoRA .safetensors is refused before anything is placed, with the reason; nothing is left behind.
func TestStage_RejectsBadFormatsAndBadNames(t *testing.T) {
	r := newRig(t, nil)
	for _, tc := range []struct {
		name string
		file string
		body []byte
		msg  string
	}{
		{"not safetensors", "a.safetensors", lorafiletest.Garbage(), ".safetensors の形式ではありません"},
		{"cut short", "a.safetensors", lorafiletest.Truncated(), ".safetensors の形式ではありません"},
		{"huge header length", "a.safetensors", lorafiletest.HugeHeaderLength(), "ヘッダーの長さ"},
		{"a checkpoint", "a.safetensors", lorafiletest.Checkpoint(), "LoRA ではなく"},
		{"not a LoRA", "a.safetensors", lorafiletest.File([]string{"x.y"}, nil), "LoRA のファイルとして扱えません"},
		{"pickle name", "a.ckpt", lorafiletest.LoRA(nil), "拡張子が .safetensors"},
		{"path traversal", "../../evil.safetensors", lorafiletest.LoRA(nil), "使えない文字"},
		{"a path with a separator", "sub/dir.safetensors", lorafiletest.LoRA(nil), "使えない文字"},
		{"a windows path", `C:\x\a.safetensors`, lorafiletest.LoRA(nil), "使えない文字"},
		{"no name", "", lorafiletest.LoRA(nil), "ファイル名がありません"},
		{"empty", "a.safetensors", nil, "空のファイル"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.stage(tc.file, tc.body)
			assert.Contains(t, refusal(t, err), tc.msg)
			assert.Empty(t, r.leftovers(), "a refused file is not kept")
			assert.Empty(t, r.files())
		})
	}
}

// A file over the limit is refused when the request says so, and cut off when it only turns out as it is read; nothing is kept either way.
func TestStage_EnforcesTheSizeLimit(t *testing.T) {
	r := newRig(t, func(d *loraimport.Deps) { d.MaxBytes = 4096 })
	big := lorafiletest.Padded(lorafiletest.LoRATensors, nil, 8192)

	_, err := r.svc.Stage(context.Background(), "a.safetensors", int64(len(big)), bytes.NewReader(big))
	assert.Contains(t, refusal(t, err), "ファイルが大きすぎます")

	_, err = r.svc.Stage(context.Background(), "a.safetensors", 0, io.MultiReader(bytes.NewReader(big), iotest.ErrReader(errors.New("must not be read this far"))))
	assert.Contains(t, refusal(t, err), "ファイルが大きすぎます", "an announced size of 0 does not get a large file in")
	assert.Empty(t, r.leftovers())
}

// An upload that breaks off, or a disk without the room, leaves nothing behind and says what happened.
func TestStage_AFailedTransferLeavesNothing(t *testing.T) {
	r := newRig(t, nil)
	body := lorafiletest.LoRA(nil)
	_, err := r.svc.Stage(context.Background(), "a.safetensors", 0, io.MultiReader(bytes.NewReader(body[:20]), iotest.ErrReader(errors.New("connection reset"))))
	assert.Contains(t, refusal(t, err), "途中で切れました")
	assert.Empty(t, r.leftovers())

	r2 := newRig(t, func(d *loraimport.Deps) { d.Free = func(string) uint64 { return 10 } })
	_, err = r2.stage("a.safetensors", body)
	assert.Contains(t, refusal(t, err), "空き容量が足りません")
	assert.Empty(t, r2.leftovers())
}

// Where the bundled engine does not exist nothing is taken in: its models folder is the destination.
func TestStage_RefusesWhereThereIsNoBundledEngine(t *testing.T) {
	r := newRig(t, func(d *loraimport.Deps) { d.Supported = false })
	_, err := r.stage("a.safetensors", lorafiletest.LoRA(nil))
	assert.Contains(t, refusal(t, err), "同梱の ComfyUI が使えない環境")
	assert.False(t, r.svc.Available())
}

// A name that is taken is never overwritten: the conflict is reported with a free name, the commit is refused with the same suggestion, and the file that
// was there is untouched. With another name the same staged file is added. The same content under that name is told apart.
func TestSameNameIsNeverOverwritten(t *testing.T) {
	r := newRig(t, nil)
	old := lorafiletest.LoRA(map[string]string{"modelspec.title": "old"})
	r.put("style.safetensors", old) // a file the user put there by hand: nobody recorded it

	incoming := lorafiletest.LoRA(map[string]string{"modelspec.title": "new"})
	st := r.mustStage("style.safetensors", incoming)
	require.NotNil(t, st.Conflict)
	assert.Equal(t, "style.safetensors", st.Conflict.Name)
	assert.False(t, st.Conflict.Same)
	assert.True(t, strings.HasPrefix(st.Conflict.Suggest, "style-"+sum(incoming)[:8]) && strings.HasSuffix(st.Conflict.Suggest, ".safetensors"), st.Conflict.Suggest)

	_, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token})
	msg := refusal(t, err)
	assert.Contains(t, msg, "上書きしません")
	assert.Contains(t, msg, st.Conflict.Suggest)
	got, _ := os.ReadFile(filepath.Join(r.lorasDir(), "style.safetensors"))
	assert.Equal(t, old, got, "the file that was there is untouched")
	assert.Empty(t, r.state.Get().LocalLoRAs, "nothing was recorded")
	_, still := r.svc.Staged(st.Token)
	assert.True(t, still, "the file waits so that the user can choose another name")

	m, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token, Name: st.Conflict.Suggest})
	require.NoError(t, err)
	assert.Equal(t, "models/loras/"+st.Conflict.Suggest, m.Path)
	assert.ElementsMatch(t, []string{"style.safetensors", st.Conflict.Suggest}, r.files())
	got, _ = os.ReadFile(filepath.Join(r.lorasDir(), "style.safetensors"))
	assert.Equal(t, old, got)

	same := r.mustStage("STYLE.SafeTensors", old)
	require.NotNil(t, same.Conflict, "names are compared without case (Windows and macOS)")
	assert.True(t, same.Conflict.Same, "the same content is told apart: nothing to add")
	_, err = r.svc.Commit(context.Background(), loraimport.Input{Token: same.Token})
	assert.Contains(t, refusal(t, err), "同じ内容のファイル")
}

// A token that is unknown (the file waited too long, or was added already) is refused, and a staged file that waited past its time is thrown away.
func TestCommit_NeedsAFileThatWaits(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	r := newRig(t, func(d *loraimport.Deps) { d.Now = func() time.Time { return now } })
	_, err := r.svc.Commit(context.Background(), loraimport.Input{Token: "nope"})
	assert.Contains(t, refusal(t, err), "もう一度ファイルを選んでください")

	st := r.mustStage("a.safetensors", lorafiletest.LoRA(nil))
	_, err = r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token})
	require.NoError(t, err)
	_, err = r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token})
	assert.Error(t, err, "a file is added once")

	st2 := r.mustStage("b.safetensors", lorafiletest.LoRA(nil))
	now = now.Add(2 * time.Hour)
	_, ok := r.svc.Staged(st2.Token)
	assert.False(t, ok)
	assert.Empty(t, r.leftovers(), "the stale file is removed from the disk")
}

// The values the user types are checked: the families, the strength range, the trigger words and the license.
func TestCommit_ChecksWhatTheUserConfirmed(t *testing.T) {
	r := newRig(t, nil)
	st := r.mustStage("a.safetensors", lorafiletest.LoRA(nil))
	bad := func(m loraimport.Meta, want string) {
		t.Helper()
		_, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token, Meta: m})
		assert.Contains(t, refusal(t, err), want)
	}
	zero, over := 0.0, 1.5
	bad(loraimport.Meta{BaseModels: []string{"not-a-family"}}, "ベースモデル")
	bad(loraimport.Meta{Strength: &zero}, "推奨の強さ")
	bad(loraimport.Meta{Strength: &over}, "推奨の強さ")
	bad(loraimport.Meta{TriggerWords: "a\x01"}, "制御文字")
	bad(loraimport.Meta{Commercial: "yes"}, "商用利用")
	bad(loraimport.Meta{LicenseName: strings.Repeat("x", 500)}, "ライセンス名")
	assert.Empty(t, r.files())
}

// The license is optional and a declaration is never 商用OK: there is no way to record "yes".
func TestLicense_IsOptionalAndNeverDefaultsToCommercialOK(t *testing.T) {
	r := newRig(t, nil)
	st := r.mustStage("a.safetensors", lorafiletest.LoRA(nil))
	m, err := r.svc.Commit(context.Background(), loraimport.Input{Token: st.Token, Meta: loraimport.Meta{LicenseName: " CreativeML OpenRAIL-M ", Commercial: "conditional"}})
	require.NoError(t, err)
	assert.Equal(t, "CreativeML OpenRAIL-M", m.LicenseName)
	assert.Equal(t, "conditional", m.Commercial)
}
