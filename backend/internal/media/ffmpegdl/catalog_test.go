package ffmpegdl_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
)

func TestTheEmbeddedCatalogPinsTheTwoPlatforms(t *testing.T) {
	c := ffmpegdl.Default()
	win, ok := c.For("windows", "amd64")
	require.True(t, ok)
	mac, ok := c.For("darwin", "arm64")
	require.True(t, ok)
	for _, p := range []ffmpegdl.Platform{win, mac} {
		assert.Equal(t, "9.0.2", p.Version)
		assert.Equal(t, "GPL-3.0-or-later", p.License.ID, "libx264 / libx265 need a GPL build: the license is stated, not hidden")
		assert.Len(t, p.Members(), 2)
	}
	assert.Equal(t, []string{"ffmpeg.exe", "ffprobe.exe"}, []string{win.Members()[0].Name, win.Members()[1].Name})
	assert.Equal(t, []string{"ffmpeg", "ffprobe"}, []string{mac.Members()[0].Name, mac.Members()[1].Name})
	assert.Equal(t, "https://github.com/GyanD/codexffmpeg/releases/download/9.0.2/ffmpeg-9.0.2-essentials_build.zip", win.Archives[0].URL)
	assert.Equal(t, "60f467265b1e312373dbcd92200c2618a74850f98d3d078e94296bb3fa2047ba", win.Archives[0].SHA256,
		"the same SHA-256 as the publisher's .sha256 file (spec/architecture/ffmpeg-download.md)")
	assert.Equal(t, "c8ed4c4e6978a03c485edbfe4e0a5dc2380f8a30bba5150531b31b094492d924", mac.Archives[0].SHA256)
	assert.Equal(t, "fcbe839537485eaee7a7a8bc5cbc0f90d53617e80943e8a5b2e31cb851197ea6", mac.Archives[1].SHA256)
	for _, a := range append(win.Archives, mac.Archives...) {
		assert.True(t, strings.HasPrefix(a.URL, "https://"))
		assert.NotContains(t, a.URL, "latest", "a pinned release, never a moving one")
		assert.Contains(t, a.URL, "9.0.2")
	}
}

func TestNoBuildIsPinnedForTheOtherPlatforms(t *testing.T) {
	c := ffmpegdl.Default()
	for _, p := range [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "arm64"}, {"windows", "386"}, {"darwin", "amd64"}} {
		_, ok := c.For(p[0], p[1])
		assert.False(t, ok, "%v", p)
	}
}

func TestSizes(t *testing.T) {
	win, _ := ffmpegdl.Default().For("windows", "amd64")
	assert.Equal(t, int64(114768076), win.DownloadBytes())
	assert.Equal(t, int64(105423872+105221120), win.ExtractedBytes())
	mac, _ := ffmpegdl.Default().For("darwin", "arm64")
	assert.Equal(t, int64(28395699+28317701), mac.DownloadBytes())
}

// mutate parses the embedded catalog through JSON, lets f change it, and validates the result.
func mutate(t *testing.T, f func(c map[string]any)) error {
	t.Helper()
	b, err := json.Marshal(ffmpegdl.Default())
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	f(m)
	b, err = json.Marshal(m)
	require.NoError(t, err)
	_, err = ffmpegdl.Parse(b)
	return err
}

func archive(m map[string]any, key string, i int) map[string]any {
	return m["platforms"].(map[string]any)[key].(map[string]any)["archives"].([]any)[i].(map[string]any)
}

func TestValidateRefusesACatalogThatIsNotPinned(t *testing.T) {
	require.NoError(t, mutate(t, func(map[string]any) {}))
	win := "windows/amd64"
	cases := map[string]func(m map[string]any){
		"http url": func(m map[string]any) {
			a := archive(m, win, 0)
			a["url"] = strings.Replace(a["url"].(string), "https", "http", 1)
		},
		"a latest url": func(m map[string]any) {
			a := archive(m, win, 0)
			a["url"] = "https://github.com/GyanD/codexffmpeg/releases/latest/download/x.zip"
		},
		"another host":       func(m map[string]any) { a := archive(m, win, 0); a["url"] = "https://example.com/ffmpeg.zip" },
		"a lookalike host":   func(m map[string]any) { a := archive(m, win, 0); a["url"] = "https://evilgithub.com/9.0.2/ffmpeg.zip" },
		"a query":            func(m map[string]any) { a := archive(m, win, 0); a["url"] = a["url"].(string) + "?x=1" },
		"format not the url": func(m map[string]any) { archive(m, win, 0)["format"] = "tar.gz" },
		"unknown format":     func(m map[string]any) { archive(m, win, 0)["format"] = "7z" },
		"no archive sha":     func(m map[string]any) { archive(m, win, 0)["sha256"] = "" },
		"an upper-case sha":  func(m map[string]any) { a := archive(m, win, 0); a["sha256"] = strings.ToUpper(a["sha256"].(string)) },
		"a short sha":        func(m map[string]any) { archive(m, win, 0)["sha256"] = "abc" },
		"no size":            func(m map[string]any) { archive(m, win, 0)["size"] = 0 },
		"no member sha":      func(m map[string]any) { archive(m, win, 0)["members"].([]any)[0].(map[string]any)["sha256"] = "" },
		"no member size":     func(m map[string]any) { archive(m, win, 0)["members"].([]any)[0].(map[string]any)["size"] = 0 },
		"a member with a path": func(m map[string]any) {
			archive(m, win, 0)["members"].([]any)[0].(map[string]any)["name"] = "bin/ffmpeg.exe"
		},
		"a traversing entry": func(m map[string]any) {
			archive(m, win, 0)["members"].([]any)[0].(map[string]any)["entry"] = "../ffmpeg.exe"
		},
		"wrong member names":  func(m map[string]any) { archive(m, win, 0)["members"].([]any)[0].(map[string]any)["name"] = "ffmpeg" },
		"one executable only": func(m map[string]any) { a := archive(m, win, 0); a["members"] = a["members"].([]any)[:1] },
		"no members":          func(m map[string]any) { archive(m, win, 0)["members"] = []any{} },
		"no archives": func(m map[string]any) {
			m["platforms"].(map[string]any)[win].(map[string]any)["archives"] = []any{}
		},
		"no license": func(m map[string]any) {
			m["platforms"].(map[string]any)[win].(map[string]any)["license"] = map[string]any{"id": "", "name": "", "url": ""}
		},
		"an unsupported platform": func(m map[string]any) {
			p := m["platforms"].(map[string]any)
			p["linux/amd64"] = p[win]
		},
		"no platform": func(m map[string]any) { m["platforms"] = map[string]any{} },
		"the same archive twice": func(m map[string]any) {
			p := m["platforms"].(map[string]any)["darwin/arm64"].(map[string]any)
			a := p["archives"].([]any)
			a[1].(map[string]any)["url"] = a[0].(map[string]any)["url"]
		},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, mutate(t, f))
		})
	}
}

func TestParseRefusesUnknownFields(t *testing.T) {
	_, err := ffmpegdl.Parse([]byte(`{"platforms":{},"extra":1}`))
	assert.Error(t, err)
}
