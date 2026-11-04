package assetstest_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Issue #1026: the face check models are an asset set of the catalog that installs into the model folder the face engine reads. The files, hashes, sizes and licenses
// are the ones read from the primary source (opencv/opencv_zoo at the commit scripts/fetch-face-models.sh also pins).
func TestTheFaceModelsAreInTheCatalogPinnedToOneCommitOfTheirPrimarySource(t *testing.T) {
	c := catalog.Default()
	require.Empty(t, catalog.Validate(c, localcfg.TemplateByID), "the catalog with the face models validates")
	s := setOf(t, c, "onnx-face-models")
	require.Len(t, s.Assets, 2)

	want := map[string]struct {
		sha     string
		size    int64
		license string
		dir     string
	}{
		facesim.DetectorFile:   {"8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4", 232589, "mit-yunet", "face_detection_yunet"},
		facesim.RecognizerFile: {"0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79", 38696353, "apache-2.0-sface", "face_recognition_sface"},
	}
	const commit = "47534e27c9851bb1128ccc0102f1145e27f23f98"
	for _, a := range s.Assets {
		w, ok := want[a.Dest]
		require.True(t, ok, "an unexpected file %s", a.Dest)
		assert.True(t, a.Required, a.Dest)
		assert.Equal(t, w.sha, a.SHA256, a.Dest)
		assert.Equal(t, w.size, a.Size, a.Dest)
		assert.Equal(t, w.license, a.License, a.Dest)
		assert.Equal(t, "https://github.com/opencv/opencv_zoo/raw/"+commit+"/models/"+w.dir+"/"+a.Dest, a.URL)
		assert.Contains(t, a.Source.URL, commit, "the source page is at the same commit")
	}
	assert.Len(t, s.Assets, len(want))
	assert.Equal(t, int64(232589+38696353), s.TotalSize())
}

// The licenses are the primary source's: YuNet MIT, SFace Apache-2.0 (a LICENSE file in each directory at the pinned commit). SFace carries the notice that the
// training data's rights are not stated, with the answer "yes" for the weights' license; nothing more is claimed than the sources say.
func TestTheFaceModelLicensesSayWhatThePrimarySourceSaysAndNoMore(t *testing.T) {
	s := setOf(t, catalog.Default(), "onnx-face-models")
	yunet, ok := s.License("mit-yunet")
	require.True(t, ok)
	sface, ok := s.License("apache-2.0-sface")
	require.True(t, ok)

	assert.Equal(t, modellicense.Yes, yunet.Commercial)
	assert.Equal(t, modellicense.Yes, sface.Commercial)
	assert.Empty(t, yunet.Notice)
	assert.Contains(t, sface.Notice, "学習データ", "the training data's rights are not stated by the primary source")
	assert.Contains(t, sface.Notice, "確かめられていません")
	assert.Regexp(t, regexp.MustCompile(`^https://github\.com/opencv/opencv_zoo/blob/[0-9a-f]{40}/models/face_detection_yunet/LICENSE$`), yunet.URL)
	assert.Regexp(t, regexp.MustCompile(`^https://github\.com/opencv/opencv_zoo/blob/[0-9a-f]{40}/models/face_recognition_sface/LICENSE$`), sface.URL)
	assert.Contains(t, yunet.Name, "MIT")
	assert.Contains(t, sface.Name, "Apache")
	assert.Empty(t, s.Unclear(), "both licenses answer yes for the weights")
}

// github.com/<repo>/raw/<commit>/ is an accepted pinned form (it follows a Git LFS file to its content); a branch is not.
func TestAGitHubRawURLIsAcceptedOnlyAtACommit(t *testing.T) {
	const commit = "47534e27c9851bb1128ccc0102f1145e27f23f98"
	for url, valid := range map[string]bool{
		"https://github.com/opencv/opencv_zoo/raw/" + commit + "/models/a/b.onnx":  true,
		"https://github.com/opencv/opencv_zoo/raw/main/models/a/b.onnx":            false,
		"https://github.com/opencv/opencv_zoo/blob/" + commit + "/models/a/b.onnx": false,
		"https://example.com/opencv/opencv_zoo/raw/" + commit + "/models/a/b.onnx": false,
	} {
		c := catalog.Default()
		set := setOf(t, c, "onnx-face-models")
		set.Assets[0].URL = url
		errs := catalog.Validate(c, localcfg.TemplateByID)
		if valid {
			assert.Empty(t, errs, url)
		} else {
			assert.NotEmpty(t, errs, url)
		}
	}
}
