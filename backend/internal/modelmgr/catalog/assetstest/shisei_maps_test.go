// The pose library's depth and normal maps and the ControlNet models that read them (Issue #1020). They are apart from assets_test.go
// because that file is at the line limit (linterly).
package assetstest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// The ControlNet weights for the depth and the normal maps of the pose library (Issue #1020) are the files that were checked on Hugging Face: the
// commit, the SHA-256 and the size of the LFS file (its API and the x-linked-etag header), Apache-2.0 on the model card, so commercial use is allowed
// next to the base SDXL's (OpenRAIL++-M).
func TestControlNetModelsForDepthAndNormalArePinnedToTheCheckedFiles(t *testing.T) {
	c := catalog.Default()
	for id, want := range map[string]struct {
		repo, commit, sha string
		size              int64
		file, template    string
	}{
		"sdxl-controlnet-depth": {"xinsir/controlnet-depth-sdxl-1.0", "961552fa2913e3aa82cdfb32cb51bf182d95207c", "ea3770af4c33ea8ba8dccc0106ec5d5c5c6e504eb382119a3ccac3c9d973d344", 2502139104,
			"xinsir-controlnet-depth-sdxl-1.0.safetensors", "txt2img-sdxl-controlnet-depth"},
		"sdxl-controlnet-normal": {"xinsir/controlnet-union-sdxl-1.0", "801a4a3fa3d4c936f4feea95b98607bc6726f80c", "a9e13fd61f3193887791c8a0dd07a07202174dc47d5ddaea94ea1344f07c7467", 2512030408,
			"xinsir-controlnet-union-sdxl-1.0.safetensors", "txt2img-sdxl-controlnet-normal"},
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		require.Len(t, m.Files, 2, id)
		assert.Equal(t, "sd_xl_base_1.0.safetensors", m.Files[0].Name, id+": the base shares the file of sdxl-base")
		f := m.Files[1]
		assert.Equal(t, want.file, f.Name, id)
		assert.Equal(t, "controlnet", f.Folder, id)
		assert.Equal(t, "https://huggingface.co/"+want.repo+"/resolve/"+want.commit+"/diffusion_pytorch_model.safetensors", f.URL, id)
		assert.Equal(t, want.sha, f.SHA256, id)
		assert.Equal(t, want.size, f.Size, id)
		assert.Equal(t, modellicense.Yes, m.License.Commercial, id)
		require.Len(t, m.License.Components, 2, id)
		assert.Equal(t, "Apache License 2.0", m.License.Components[1].Name, id)
		assert.Contains(t, m.License.Components[1].URL, want.commit, id+": the license is read at the pinned commit")
		require.Len(t, m.Workflows, 1, id)
		assert.Equal(t, want.template, m.Workflows[0].Template, id)
	}
}

// The depth and normal maps of the pose library (Issue #1020) are the release's zips, pinned by the SHA-256 and the size that were checked
// against the download (not only the digest GitHub shows), under the license of the images (CC0 1.0, commercial use allowed), and they hold
// the maps of the poses the line art holds: the general std and upper series, nothing of the zips' sexy and duo files.
func TestPoseLibraryDepthAndNormalMapsArePinnedAndHoldOnlyThePosesOfTheLineArt(t *testing.T) {
	s := setOf(t, catalog.Default(), "shisei-poses")
	line := map[string]bool{} // the file names of the line art
	for _, a := range s.Assets {
		for _, m := range a.Members {
			if strings.HasPrefix(m.Entry, "line/") {
				line[strings.TrimPrefix(m.Entry, "line/")] = true
			}
		}
	}
	require.Len(t, line, 414)

	for kind, want := range map[string]struct {
		file, sha string
		size      int64
		members   int
	}{
		"depth":  {"shisei-depth-v0.4.1.zip", "d572572ade47e73d5450da94836abea64c7a0ea9f266e8d162e512cd40e0a23d", 7110041, 335},
		"normal": {"shisei-normal-v0.4.1.zip", "7271104b0e5c106eda147ee2204e2f08484cba44826ebb2b9c1211256bc2ff1e", 16185565, 335},
	} {
		a := assetNamed(t, *s, map[string]string{"depth": "深度", "normal": "法線"}[kind])
		assert.Equal(t, "https://github.com/wquguru/shisei/releases/download/v0.4.1/"+want.file, a.URL, kind)
		assert.Equal(t, want.sha, a.SHA256, kind)
		assert.Equal(t, want.size, a.Size, kind)
		assert.True(t, a.Required, kind)
		assert.Equal(t, catalog.FormatZip, a.Format, kind)
		l, ok := s.License(a.License)
		require.True(t, ok, kind)
		assert.Equal(t, modellicense.Yes, l.Commercial, kind)
		assert.Contains(t, l.Name, "CC0", kind)

		require.Len(t, a.Members, want.members, kind)
		for _, m := range a.Members {
			name := strings.TrimPrefix(m.Entry, kind+"/")
			assert.NotEqual(t, m.Entry, name, "%s: an entry of the %s/ directory of the zip", m.Entry, kind)
			assert.Equal(t, m.Entry, m.Dest, "the maps are installed below depth/ and normal/ of the library, apart from the line art")
			assert.Regexp(t, `^(std_P|upper_U)[0-9]{3}_(front|diag|side|above|below)\.webp$`, name, "no sexy or duo file, whatever the zip holds")
			assert.True(t, line[name], "%s: a map of a pose that has no line art here (not general, or not a pose of the gallery)", m.Entry)
		}
	}
}
