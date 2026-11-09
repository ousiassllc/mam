package loraimport_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile/lorafiletest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

const hfCommit = "0123456789abcdef0123456789abcdef01234567"

// A LoRA the user added and a file from Hugging Face can be the same file (the same bytes under the same name); the file then stays while either record
// holds it, whichever is deleted first, and a different file under that name is never put over it.
func TestALocalLoRAAndAnImportedFileShareTheFileUntilBothAreGone(t *testing.T) {
	body := lorafiletest.LoRA(map[string]string{"modelspec.title": "shared"})
	other := lorafiletest.LoRA(map[string]string{"modelspec.title": "other"})
	const rel = "models/loras/style.safetensors"
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		first func(r *mmtest.Rig) error
		last  func(r *mmtest.Rig) error
	}{
		{"the LoRA first", func(r *mmtest.Rig) error { return r.Svc.LoRAs.Delete(ctx, rel) }, func(r *mmtest.Rig) error { return r.Svc.DeleteUserModel(ctx, rel) }},
		{"the import first", func(r *mmtest.Rig) error { return r.Svc.DeleteUserModel(ctx, rel) }, func(r *mmtest.Rig) error { return r.Svc.LoRAs.Delete(ctx, rel) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mmtest.New(t)
			r.InstallEngine()
			_, err := r.Svc.LoRAs.Import(ctx, "style.safetensors", int64(len(body)), bytes.NewReader(body), loraimport.Input{}, false)
			require.NoError(t, err)
			r.HF.Add(hftest.Repo{ID: "o/same", Commit: hfCommit, License: "mit", Files: []hftest.File{{Path: "style.safetensors", Body: body}}})
			r.HF.Add(hftest.Repo{ID: "o/diff", Commit: hfCommit, License: "mit", Files: []hftest.File{{Path: "style.safetensors", Body: other}}})
			install := func(repo string) (modelmanager.JobRef, error) {
				return r.Svc.InstallHFFile(ctx, modelmanager.HFInstallInput{Repo: repo, Commit: hfCommit, Path: "style.safetensors", Accept: true, TermsVersion: hfplan.TermsVersion})
			}
			file := r.Layout.ModelPath("models/loras/style.safetensors")

			_, err = install("o/diff")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "上書きしません", "a different file is never put over the LoRA")

			ref, err := install("o/same")
			require.NoError(t, err, "the same bytes under the same name are the same file")
			r.Wait(ref.JobID)
			require.Len(t, r.Svc.UserModels(), 1)
			require.Len(t, r.Svc.LoRAs.List(), 1)

			require.NoError(t, tc.first(r))
			assert.FileExists(t, file, "the other record still holds the file")
			require.NoError(t, tc.last(r))
			assert.NoFileExists(t, file, "nobody holds it any more")
		})
	}
}
