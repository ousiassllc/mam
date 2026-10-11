package router_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/router"
)

func (a *app) importBundle(data []byte) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("bundle", "project.zip")
	require.NoError(a.t, err)
	_, err = part.Write(data)
	require.NoError(a.t, err)
	require.NoError(a.t, w.Close())
	return a.request(http.MethodPost, "/projects/import", buf.Bytes(), w.FormDataContentType(), true)
}

func zipOf(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func zipEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		var b bytes.Buffer
		_, err = b.ReadFrom(rc)
		require.NoError(t, err)
		_ = rc.Close()
		out[f.Name] = b.Bytes()
	}
	return out
}

func TestBundle_ExportImportRoundTripsTheProjectAndItsFilesToAnotherInstance(t *testing.T) {
	src := newGenApp(t, true)
	src.execSQL(`INSERT INTO world_state_version (id, project_id, version_number, state) VALUES ('w1', ?, 1, '{"camera":{"lens":"35mm"}}')`, src.project)
	src.generateAndWait()

	rec := src.get("/projects/"+src.project+"/bundle", false)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), src.project)
	entries := zipEntries(t, rec.Body.Bytes())
	assert.Contains(t, entries, "manifest.json")
	assert.Contains(t, entries, "data.json")
	var files int
	for name := range entries {
		if len(name) > len("assets/") && name[:len("assets/")] == "assets/" {
			files++
			assert.Contains(t, name, "assets/"+src.project+"/", "files keep their asset-relative path")
		}
	}
	assert.Equal(t, 3, files, "the video of every Take")

	dst := newApp(t)
	imp := dst.importBundle(rec.Body.Bytes())
	require.Equal(t, http.StatusCreated, imp.Code, imp.Body.String())
	assert.Equal(t, "/projects/"+src.project, imp.Header().Get("HX-Redirect"))

	// The Project, its Shot spec history, Takes, World State and files arrived.
	for _, q := range []string{
		`SELECT COUNT(*) FROM project`, `SELECT COUNT(*) FROM scene`, `SELECT COUNT(*) FROM shot`,
		`SELECT COUNT(*) FROM prompt_version`, `SELECT COUNT(*) FROM take`, `SELECT COUNT(*) FROM asset`,
		`SELECT COUNT(*) FROM world_state_version`, `SELECT COUNT(*) FROM chat_message`, `SELECT COUNT(*) FROM cost_ledger_entry`,
	} {
		var want, got int
		require.NoError(t, src.db.Reader().QueryRow(q).Scan(&want), q)
		require.NoError(t, dst.db.Reader().QueryRow(q).Scan(&got), q)
		assert.Equal(t, want, got, q)
	}
	assert.Equal(t, http.StatusOK, dst.get("/projects/"+src.project, false).Code)
	assert.Equal(t, http.StatusOK, dst.get("/projects/"+src.project+"/world-state", false).Code)
	var assetID, filePath string
	require.NoError(t, dst.db.Reader().QueryRow(`SELECT id, file_path FROM asset WHERE type = 'video' LIMIT 1`).Scan(&assetID, &filePath))
	video := dst.get("/projects/"+src.project+"/assets/"+assetID, false)
	require.Equal(t, http.StatusOK, video.Code)
	assert.Equal(t, "\x00\x00\x00\x18ftypisom", video.Body.String())
	var takeID string
	require.NoError(t, dst.db.Reader().QueryRow(`SELECT id FROM take LIMIT 1`).Scan(&takeID))
	assert.Equal(t, http.StatusOK, dst.get("/takes/"+takeID+"/trace", true).Code, "the trace survives the move")

	// Importing again never touches the Project that exists.
	again := dst.importBundle(rec.Body.Bytes())
	assert.Equal(t, http.StatusConflict, again.Code)
	assert.Contains(t, again.Body.String(), "すでにあります")
	_, err := os.Stat(filepath.Join(dst.assets, filepath.FromSlash(filePath)))
	assert.NoError(t, err)

	assert.Equal(t, http.StatusNotFound, src.get("/projects/nope/bundle", false).Code)
}

func TestBundle_AnUnusableFileIsRefusedAndNothingIsLeftBehind(t *testing.T) {
	dst := newApp(t)
	manifest := func(project string) []byte {
		b, err := json.Marshal(map[string]any{"format": "mam-project-bundle", "version": 1, "project_id": project})
		require.NoError(t, err)
		return b
	}
	project := `{"id":"proj1","state":"DRAFT","mode":"auto","confirmation_level":"key_check","production_template":"product_cm","quality_policy":"balanced"}`
	data := func(tables string) []byte {
		return []byte(`{"project_id":"proj1","tables":[{"name":"project","rows":[` + project + `]}` + tables + `]}`)
	}
	cases := map[string][]byte{
		"not a zip":   []byte("hello"),
		"no manifest": zipOf(t, map[string][]byte{"data.json": data("")}),
		"wrong format": zipOf(t, map[string][]byte{
			"manifest.json": []byte(`{"format":"other","version":1,"project_id":"proj1"}`), "data.json": data(""),
		}),
		"newer version": zipOf(t, map[string][]byte{
			"manifest.json": []byte(`{"format":"mam-project-bundle","version":99,"project_id":"proj1"}`), "data.json": data(""),
		}),
		"ids differ": zipOf(t, map[string][]byte{"manifest.json": manifest("other"), "data.json": data("")}),
		"unknown table": zipOf(t, map[string][]byte{
			"manifest.json": manifest("proj1"), "data.json": data(`,{"name":"job","rows":[{"id":"j"}]}`),
		}),
		"unknown column": zipOf(t, map[string][]byte{
			"manifest.json": manifest("proj1"), "data.json": data(`,{"name":"brief","rows":[{"id":"b","project_id":"proj1","nope":1}]}`),
		}),
		"row of another project": zipOf(t, map[string][]byte{
			"manifest.json": manifest("proj1"), "data.json": data(`,{"name":"brief","rows":[{"id":"b","project_id":"someone-else"}]}`),
		}),
		"dangling foreign key": zipOf(t, map[string][]byte{
			"manifest.json": manifest("proj1"), "data.json": data(`,{"name":"shot","rows":[{"id":"s","scene_id":"missing","order_index":0}]}`),
		}),
		"file outside the project": zipOf(t, map[string][]byte{
			"manifest.json": manifest("proj1"), "data.json": data(""), "assets/../../evil.txt": []byte("x"),
		}),
		"file of another project": zipOf(t, map[string][]byte{
			"manifest.json": manifest("proj1"), "data.json": data(""), "assets/other/a.png": []byte("x"),
		}),
		"asset path escapes": zipOf(t, map[string][]byte{
			"manifest.json": manifest("proj1"),
			"data.json":     data(`,{"name":"asset","rows":[{"id":"a","project_id":"proj1","type":"image","file_path":"proj1/../../x.png"}]}`),
		}),
	}
	for name, body := range cases {
		rec := dst.importBundle(body)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `id="project-import-form"`, name)
		assert.Contains(t, rec.Body.String(), `role="alert"`, name)
	}
	var n int
	require.NoError(t, dst.db.Reader().QueryRow(`SELECT COUNT(*) FROM project`).Scan(&n))
	assert.Zero(t, n, "every refusal rolled back")
	_, err := os.Stat(filepath.Join(dst.assets, "proj1"))
	assert.True(t, os.IsNotExist(err), "no file was left behind")
	_, err = os.Stat(filepath.Join(filepath.Dir(dst.assets), "evil.txt"))
	assert.True(t, os.IsNotExist(err))

	missing := dst.request(http.MethodPost, "/projects/import", nil, "", true)
	assert.Equal(t, http.StatusBadRequest, missing.Code, "not a multipart body at all")
}

func TestBundle_AnAudioTimelineNamingFilesOutsideTheProjectIsRefused(t *testing.T) {
	manifest := []byte(`{"format":"mam-project-bundle","version":1,"project_id":"proj1"}`)
	project := `{"id":"proj1","state":"DRAFT","mode":"auto","confirmation_level":"key_check","production_template":"product_cm","quality_policy":"balanced"}`
	// The Audio Timeline is JSON inside a column: the files it names are the Project's own too.
	cases := map[string]string{
		"mix path escapes":             `{"mix_path":"../../x.mp4"}`,
		"mix path of another project":  `{"mix_path":"other/mix.m4a"}`,
		"mix path escapes the project": `{"mix_path":"proj1/../../x.mp4"}`,
		"audio clip path escapes":      `{"clips":[{"path":"proj1/../../x.mp3"}]}`,
		"lip sync path is absolute":    `{"lip_sync":[{"clip_index":1,"take_id":"t","path":"/etc/passwd"}]}`,
		"not a timeline":               `[]`,
	}
	for name, timeline := range cases {
		dst := newApp(t)
		quoted, err := json.Marshal(timeline)
		require.NoError(t, err)
		data := `{"project_id":"proj1","tables":[{"name":"project","rows":[` + project + `]},` +
			`{"name":"audio_timeline_version","rows":[{"id":"av","project_id":"proj1","version_number":1,"timeline":` + string(quoted) + `}]}]}`

		rec := dst.importBundle(zipOf(t, map[string][]byte{"manifest.json": manifest, "data.json": []byte(data)}))

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		var n int
		require.NoError(t, dst.db.Reader().QueryRow(`SELECT COUNT(*) FROM project`).Scan(&n))
		assert.Zero(t, n, name)
	}
}

func TestBundle_AnAudioTimelineOfTheProjectsOwnFilesImports(t *testing.T) {
	dst := newApp(t)
	manifest := []byte(`{"format":"mam-project-bundle","version":1,"project_id":"proj1"}`)
	project := `{"id":"proj1","state":"DRAFT","mode":"auto","confirmation_level":"key_check","production_template":"product_cm","quality_policy":"balanced"}`
	timeline, err := json.Marshal(`{"mix_path":"proj1/audio/e/r/mix.m4a","clips":[{"path":"proj1/audio/e/r/a.mp3"},{"path":""}],"lip_sync":[{"path":"proj1/audio/e/r/lipsync-01.mp4"}]}`)
	require.NoError(t, err)
	data := `{"project_id":"proj1","tables":[{"name":"project","rows":[` + project + `]},` +
		`{"name":"audio_timeline_version","rows":[{"id":"av","project_id":"proj1","version_number":1,"timeline":` + string(timeline) + `}]}]}`

	rec := dst.importBundle(zipOf(t, map[string][]byte{"manifest.json": manifest, "data.json": []byte(data)}))

	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestBundle_ImportFormAndExportLinkAreOnTheirPages(t *testing.T) {
	a := newApp(t)
	id := a.createProject("美容液の縦型15秒CMを作りたい")
	list := a.get("/projects", false).Body.String()
	assert.Contains(t, list, `hx-post="/projects/import"`)
	assert.Contains(t, list, `name="bundle"`)
	settings := a.get("/projects/"+id+"/settings", false).Body.String()
	assert.Contains(t, settings, `href="/projects/`+id+`/bundle"`)
}

func TestBundle_ImportsOfTheSameBundleAtTheSameTimeLeaveTheWinnersFilesAlone(t *testing.T) {
	src := newGenApp(t, true)
	src.generateAndWait()
	bundle := src.get("/projects/"+src.project+"/bundle", false).Body.Bytes()
	dst := newApp(t)

	const imports = 6
	codes := make([]int, imports)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = dst.importBundle(bundle).Code
		}()
	}
	wg.Wait()

	created, conflicts := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		}
	}
	assert.Equal(t, 1, created, "codes: %v", codes)
	assert.Equal(t, imports-1, conflicts, "codes: %v", codes)

	rows, err := dst.db.Reader().Query(`SELECT file_path FROM asset`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var paths int
	for rows.Next() {
		var p string
		require.NoError(t, rows.Scan(&p))
		content, err := os.ReadFile(filepath.Join(dst.assets, filepath.FromSlash(p)))
		require.NoError(t, err, "the winner's file is still there: %s", p)
		assert.NotEmpty(t, content)
		paths++
	}
	assert.NotZero(t, paths)

	left, err := filepath.Glob(filepath.Join(dst.assets, ".import-*"))
	require.NoError(t, err)
	assert.Empty(t, left, "no staging directory stays behind")
}

func TestBundle_ARefusedImportLeavesNoDirectoryInTheAssetsDirectory(t *testing.T) {
	dst := newApp(t)
	// The file is fine but the row is not: the database refuses after the files are in place.
	bad := zipOf(t, map[string][]byte{
		"manifest.json":        []byte(`{"format":"mam-project-bundle","version":1,"project_id":"proj1"}`),
		"data.json":            []byte(`{"project_id":"proj1","tables":[{"name":"project","rows":[{"id":"proj1","state":"NOPE"}]}]}`),
		"assets/proj1/a/b.bin": []byte("x"),
	})
	assert.Equal(t, http.StatusUnprocessableEntity, dst.importBundle(bad).Code)

	entries, err := os.ReadDir(dst.assets)
	if err != nil {
		require.True(t, os.IsNotExist(err))
		return
	}
	assert.Empty(t, entries, "neither the Project's directories nor a staging directory remain")
}

// A bundle that fails after the download started (a file that cannot be read) must not end like a
// finished download: the connection is cut, so the client sees a failed transfer instead of a
// truncated ZIP that looks complete.
func TestBundle_ExportThatFailsMidStreamIsNotDeliveredAsASuccess(t *testing.T) {
	src := newGenApp(t, true)
	src.generateAndWait()
	var filePath string
	require.NoError(t, src.db.Reader().QueryRow(`SELECT file_path FROM asset WHERE type = 'video' LIMIT 1`).Scan(&filePath))
	full := filepath.Join(src.assets, filepath.FromSlash(filePath))
	// Opening a directory works, reading it fails: the failure comes after the first bytes are written.
	require.NoError(t, os.Remove(full))
	require.NoError(t, os.Mkdir(full, 0o750))

	server := httptest.NewServer(src.engine)
	defer server.Close()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/projects/"+src.project+"/bundle", http.NoBody)
	require.NoError(t, err)
	req.AddCookie(src.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		_, err = io.ReadAll(resp.Body)
	}
	assert.Error(t, err, "the client must see the export fail")
}

// syncBuffer is a log sink the server goroutine writes while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The connection cut of a failed export leaves its trace: one error line with the request and the
// cause of the failure (the panic that drops the connection bypasses net/http's own logging).
func TestBundle_ExportThatFailsMidStreamIsLoggedWithItsCause(t *testing.T) {
	src := newGenApp(t, true)
	src.generateAndWait()
	var filePath string
	require.NoError(t, src.db.Reader().QueryRow(`SELECT file_path FROM asset WHERE type = 'video' LIMIT 1`).Scan(&filePath))
	full := filepath.Join(src.assets, filepath.FromSlash(filePath))
	require.NoError(t, os.Remove(full))
	require.NoError(t, os.Mkdir(full, 0o750)) // opening works, reading fails

	var logs syncBuffer
	engine := router.New(src.svc, router.Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	server := httptest.NewServer(engine)
	defer server.Close()
	path := "/projects/" + src.project + "/bundle"
	req, err := http.NewRequest(http.MethodGet, server.URL+path, http.NoBody)
	require.NoError(t, err)
	req.AddCookie(src.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		_, err = io.ReadAll(resp.Body)
	}
	require.Error(t, err, "the client still sees the export fail")

	out := logs.String()
	assert.Equal(t, 1, strings.Count(out, "http request"), out)
	assert.Contains(t, out, "level=ERROR")
	assert.Contains(t, out, "method=GET")
	assert.Contains(t, out, "path="+path)
	assert.Contains(t, out, "is a directory", "the cause of the failed file")
}
