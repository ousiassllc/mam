package hf_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

func TestParseInput_AcceptsTheFormsOfARepository(t *testing.T) {
	const commit = "462165984030d82259a11f4367a4eed129e94a7b"
	cases := map[string]hf.Ref{
		"stabilityai/stable-diffusion-xl-base-1.0":                                       {Repo: "stabilityai/stable-diffusion-xl-base-1.0", Kind: hf.KindRepo},
		"  Comfy-Org/flux1-dev  ":                                                        {Repo: "Comfy-Org/flux1-dev", Kind: hf.KindRepo},
		"https://huggingface.co/stabilityai/sdxl-turbo":                                  {Repo: "stabilityai/sdxl-turbo", Kind: hf.KindRepo},
		"https://huggingface.co/stabilityai/sdxl-turbo/":                                 {Repo: "stabilityai/sdxl-turbo", Kind: hf.KindRepo},
		"https://huggingface.co/Comfy-Org/flux1-dev/tree/main":                           {Repo: "Comfy-Org/flux1-dev", Rev: "main", Kind: hf.KindTree},
		"https://huggingface.co/Comfy-Org/flux1-dev/tree/" + commit + "/split_files/vae": {Repo: "Comfy-Org/flux1-dev", Rev: commit, Path: "split_files/vae", Kind: hf.KindTree},
		"https://huggingface.co/a/b/blob/main/x/y.safetensors":                           {Repo: "a/b", Rev: "main", Path: "x/y.safetensors", Kind: hf.KindBlob},
		"https://huggingface.co/a/b/resolve/" + commit + "/ae.safetensors":               {Repo: "a/b", Rev: commit, Path: "ae.safetensors", Kind: hf.KindResolve},
		"HTTPS://HuggingFace.co/a/b":                                                     {Repo: "a/b", Kind: hf.KindRepo},
	}
	for in, want := range cases {
		got, err := hf.ParseInput(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestParseInput_RefusesWhatIsNotAHuggingFaceModelRepository(t *testing.T) {
	bad := []string{
		"",
		"just-a-name",
		"a/b/c",
		"a/../b",
		"http://huggingface.co/a/b",
		"https://hf.co/a/b",
		"https://huggingface.co.evil.example/a/b",
		"https://evil.example/huggingface.co/a/b",
		"https://user:pw@huggingface.co/a/b",
		"https://huggingface.co:8443/a/b",
		"https://huggingface.co/a/b?download=true",
		"https://huggingface.co/a/b#files",
		"https://huggingface.co/a/b/resolve/main/x.safetensors?download=true",
		"https://huggingface.co/datasets/a/b",
		"https://huggingface.co/spaces/a/b",
		"https://huggingface.co/a",
		"https://huggingface.co/a/b/discussions",
		"https://huggingface.co/a/b/blob/main",
		"https://huggingface.co/a/b/blob/main/../x.safetensors",
		"https://huggingface.co/a/b/blob/ma in/x.safetensors",
		"file:///etc/passwd",
		"ftp://huggingface.co/a/b",
		"a/b\nc",
		strings.Repeat("a", 3000),
	}
	for _, in := range bad {
		_, err := hf.ParseInput(in)
		require.Error(t, err, in)
		assert.ErrorIs(t, err, hf.ErrInput, in)
	}
}

func TestValidPath_RefusesTraversalAndOddSegments(t *testing.T) {
	assert.True(t, hf.ValidPath("split_files/vae/ae.safetensors"))
	for _, p := range []string{"", "/a", "a//b", "a/./b", "a/../b", `a\b`, "a?b", "a#b", "a\x00b", "a/"} {
		assert.False(t, hf.ValidPath(p), "%q", p)
	}
}
