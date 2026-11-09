package lorafile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile/lorafiletest"
)

func write(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.safetensors")
	require.NoError(t, os.WriteFile(p, b, 0o600))
	return p
}

// A well-formed LoRA is accepted, and what its metadata says is read as it is: the family of the base model, the trigger words the file names, the
// tags the training saw most often (a hint only), the license name. Nothing is guessed.
func TestInspect_AValidLoRAIsAcceptedAndItsMetadataRead(t *testing.T) {
	info, err := lorafile.Inspect(write(t, lorafiletest.LoRA(map[string]string{
		"ss_base_model_version": "sdxl_base_v1-0", "modelspec.architecture": "stable-diffusion-xl-v1-base/lora",
		"modelspec.trigger_phrase": "mychar, red dress", "modelspec.title": "My Char", "modelspec.license": "CC BY-NC 4.0",
		"ss_tag_frequency": `{"set1":{"1girl":30,"red dress":12,"solo":30}}`, "ss_network_dim": "32", "ss_network_alpha": "16",
	})))
	require.NoError(t, err)
	assert.Equal(t, []string{"sdxl"}, info.Metadata.BaseModels)
	assert.Equal(t, []string{"mychar", "red dress"}, info.Metadata.TriggerWords)
	assert.Equal(t, "My Char", info.Metadata.Title)
	assert.Equal(t, "CC BY-NC 4.0", info.Metadata.License)
	assert.Equal(t, []string{"1girl", "solo", "red dress"}, info.Metadata.TagCandidates)
	assert.Equal(t, "32", info.Metadata.NetworkDim)
}

// A LoRA without __metadata__ is accepted too: every field is empty (the screen says 未設定), nothing is inferred from the file name or the tensors.
func TestInspect_ALoRAWithoutMetadataSaysNothing(t *testing.T) {
	info, err := lorafile.Inspect(write(t, lorafiletest.LoRA(nil)))
	require.NoError(t, err)
	assert.True(t, info.Metadata.Empty())
	assert.Empty(t, info.Metadata.BaseModels)
	assert.Empty(t, info.Metadata.TriggerWords)
}

// A base model mam does not know (FLUX.2) is not mapped to a family it is not.
func TestReadMetadata_AnUnknownBaseModelIsNotGuessed(t *testing.T) {
	m := lorafile.ReadMetadata(map[string]string{"ss_base_model_version": "flux2_dev", "modelspec.architecture": "something-new"})
	assert.Empty(t, m.BaseModels)
	assert.Equal(t, []string{"something-new", "flux2_dev"}, m.BaseRaw)
}

func TestInspect_RejectsWhatIsNotAWellFormedLoRA(t *testing.T) {
	for name, tc := range map[string]struct {
		body []byte
		msg  string
	}{
		"text with a .safetensors name":        {lorafiletest.Garbage(), ".safetensors の形式ではありません"},
		"a header length beyond the file":      {lorafiletest.HugeHeaderLength(), "ヘッダーの長さ"},
		"a file cut short":                     {lorafiletest.Truncated(), ".safetensors の形式ではありません"},
		"data longer than the tensors cover":   {lorafiletest.Padded(lorafiletest.LoRATensors, nil, 16), "データの大きさ"},
		"a checkpoint":                         {lorafiletest.Checkpoint(), "LoRA ではなく"},
		"tensors that are not those of a LoRA": {lorafiletest.File([]string{"foo.bar", "baz"}, nil), "LoRA のファイルとして扱えません"},
		"an empty file":                        {nil, "空のファイル"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := lorafile.Inspect(write(t, tc.body))
			var rej *lorafile.Reject
			require.ErrorAs(t, err, &rej)
			assert.Contains(t, rej.Message, tc.msg)
		})
	}
}

func TestCleanName(t *testing.T) {
	ok := func(raw, want string, changed bool) {
		t.Helper()
		got, ch, err := lorafile.CleanName(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
		assert.Equal(t, changed, ch, raw)
	}
	ok("my-lora_v1.safetensors", "my-lora_v1.safetensors", false)
	ok("My LoRA (v2).SafeTensors", "My_LoRA_v2_.safetensors", true)
	ok("キャラ.safetensors", "model.safetensors", true)

	for _, bad := range []string{"", "  ", "a/b.safetensors", `a\b.safetensors`, "../x.safetensors", "x..safetensors", "x\x00.safetensors", "x.ckpt", "x.pt", "x.safetensors.exe", "noext", "x.sft"} {
		_, _, err := lorafile.CleanName(bad)
		var rej *lorafile.Reject
		assert.ErrorAs(t, err, &rej, "%q", bad)
	}
}

func TestParseWords(t *testing.T) {
	w, err := lorafile.ParseWords(" a, b、c\nb ,, d ")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c", "d"}, w)
	_, err = lorafile.ParseWords("a\x01b")
	assert.Error(t, err)
}
