package detect_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/detect"
)

func comfy(t *testing.T, body string, status int) (port int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/system_stats" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	_, p, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	port, _ = strconv.Atoi(p)
	return port
}

const stats = `{"system":{"comfyui_version":"0.42.1","embedded_python":true},"devices":[{"name":"cuda:0 NVIDIA GeForce RTX 4090","vram_total":25757220864,"vram_free":24000000000}]}`

func TestDetectFindsAComfyUIThatAnswers(t *testing.T) {
	port := comfy(t, stats, 200)
	r := detect.Detect(context.Background(), detect.Options{Ports: []int{port}})
	require.Len(t, r.Found, 1)
	assert.Equal(t, "http://127.0.0.1:"+strconv.Itoa(port), r.Found[0].URL)
	assert.Equal(t, "0.42.1", r.Found[0].Version)
	assert.False(t, r.Found[0].Desktop, "not the Desktop port")
	require.Len(t, r.Found[0].Devices, 1)
	assert.Equal(t, int64(25757220864), r.Found[0].Devices[0].VRAMTotal)
	assert.Equal(t, r.Found[0].URL, r.Recommended)

	// The port of ComfyUI Desktop marks it: an app mam does not control.
	r = detect.Detect(context.Background(), detect.Options{Ports: []int{port}, DesktopPort: port})
	require.Len(t, r.Found, 1)
	assert.True(t, r.Found[0].Desktop)
}

func TestDetectIgnoresWhatIsNotComfyUI(t *testing.T) {
	for name, tc := range map[string]struct {
		body   string
		status int
	}{
		"not json":        {"<html>", 200},
		"no version":      {`{"system":{}}`, 200},
		"an error status": {stats, 500},
		"another json":    {`{"hello":"world"}`, 200},
	} {
		t.Run(name, func(t *testing.T) {
			r := detect.Detect(context.Background(), detect.Options{Ports: []int{comfy(t, tc.body, tc.status)}})
			assert.Empty(t, r.Found)
			assert.NotNil(t, r.Found, "an empty list in the JSON")
			assert.Empty(t, r.Recommended)
		})
	}
	// A port nobody listens on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	assert.Empty(t, detect.Detect(context.Background(), detect.Options{Ports: []int{port}}).Found)
}

// The Desktop configuration is a hint that does not make anything "found".
func TestDetectReadsTheDesktopConfigAsAHint(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ComfyUI"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ComfyUI", "config.json"), []byte(`{"basePath":"D:\\ComfyUI"}`), 0o600))
	r := detect.Detect(context.Background(), detect.Options{Ports: []int{}, ConfigDir: dir})
	assert.True(t, r.Desktop.Exists)
	assert.Equal(t, `D:\ComfyUI`, r.Desktop.BasePath)
	assert.Equal(t, filepath.Join(dir, "ComfyUI", "config.json"), r.Desktop.Path)
	assert.Empty(t, r.Found)

	r = detect.Detect(context.Background(), detect.Options{Ports: []int{}, ConfigDir: t.TempDir()})
	assert.False(t, r.Desktop.Exists)
	assert.False(t, detect.Detect(context.Background(), detect.Options{Ports: []int{}}).Desktop.Exists)
}
