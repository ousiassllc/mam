package router_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var mp4Bytes = append([]byte("\x00\x00\x00\x18ftypisom"), bytes.Repeat([]byte{7}, 64)...)

// ownerWithExport is an owner instance with a Project that holds things that must never be shared
// (an API key, a face reference image, a Brief, an uploaded reference, a second Export) and one Export
// a review can be requested for.
type owner struct {
	*app
	project, exportID string
}

const (
	secretKey  = "sk-live-TOPSECRET-API-KEY"
	faceMarker = "FACE-REFERENCE-PIXELS"
	otherVideo = "OTHER-EXPORT-BYTES"
)

func newOwner(t *testing.T) *owner {
	t.Helper()
	a := newApp(t)
	require.NoError(t, a.svc.Credentials.Save("kling", secretKey))
	p := a.createProject("美容液の縦型15秒CMを作りたい")
	writeAsset := func(rel string, data []byte) {
		abs := filepath.Join(a.assets, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, data, 0o644))
	}
	writeAsset(p+"/exports/final.mp4", mp4Bytes)
	writeAsset(p+"/exports/other.mp4", append([]byte("\x00\x00\x00\x18ftypisom"), []byte(otherVideo)...))
	writeAsset(p+"/references/face.png", append(pngBytes, []byte(faceMarker)...))
	a.execSQL(`INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec) VALUES ('e1', ?, ?, '1080p', '9:16', 'h264')`, p, p+"/exports/final.mp4")
	a.execSQL(`INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec) VALUES ('e2', ?, ?, '720p', '16:9', 'h264')`, p, p+"/exports/other.mp4")
	a.execSQL(`INSERT INTO asset (id, project_id, type, file_path, mime_type) VALUES ('face1', ?, 'reference', ?, 'image/png')`, p, p+"/references/face.png")
	a.execSQL(`INSERT INTO chat_message (id, project_id, role, kind, content) VALUES ('m-secret', ?, 'user', 'text', 'SECRET-BRIEF-TEXT')`, p)
	a.execSQL(`INSERT INTO character (id, project_id, name, face_reference_asset_id) VALUES ('c1', ?, 'Mio', 'face1')`, p)
	return &owner{app: a, project: p, exportID: "e1"}
}

func (o *owner) requestReview(note string) string {
	o.t.Helper()
	rec := o.form(http.MethodPost, "/projects/"+o.project+"/reviews", url.Values{"export_id": {o.exportID}, "note": {note}})
	require.Equal(o.t, http.StatusCreated, rec.Code, rec.Body.String())
	var id string
	require.NoError(o.t, o.db.Reader().QueryRow(`SELECT id FROM review_request ORDER BY rowid DESC LIMIT 1`).Scan(&id))
	return id
}

func (o *owner) packageOf(reviewID string) []byte {
	o.t.Helper()
	rec := o.get("/projects/"+o.project+"/reviews/"+reviewID+"/package", false)
	require.Equal(o.t, http.StatusOK, rec.Code)
	assert.Equal(o.t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Contains(o.t, rec.Header().Get("Content-Disposition"), "attachment")
	return rec.Body.Bytes()
}

func (a *app) importReviewFile(path, field, name string, data []byte) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, name)
	require.NoError(a.t, err)
	_, err = part.Write(data)
	require.NoError(a.t, err)
	require.NoError(a.t, w.Close())
	return a.request(http.MethodPost, path, buf.Bytes(), w.FormDataContentType(), true)
}

// reviewerImports imports the package into a fresh instance and returns it with the inbox ID.
func reviewerImports(t *testing.T, pkg []byte) (*app, string) {
	t.Helper()
	r := newApp(t)
	rec := r.importReviewFile("/reviews/import", "package", "review.zip", pkg)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	loc := rec.Header().Get("HX-Redirect")
	require.True(t, strings.HasPrefix(loc, "/reviews/"), loc)
	return r, strings.TrimPrefix(loc, "/reviews/")
}

func TestReview_PackageCarriesTheVideoAndNothingElseOfTheProject(t *testing.T) {
	o := newOwner(t)
	id := o.requestReview("最終版です。よろしくお願いします")
	pkg := o.packageOf(id)

	entries := zipEntries(t, pkg)
	require.Len(t, entries, 2, "exactly the manifest and one video")
	assert.Equal(t, mp4Bytes, entries["video.mp4"])
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(entries["manifest.json"], &manifest))
	assert.Equal(t, "mam-review-package", manifest["format"])
	assert.Equal(t, id, manifest["review_id"])
	assert.Equal(t, "最終版です。よろしくお願いします", manifest["note"])
	assert.NotEmpty(t, manifest["project_name"])
	assert.ElementsMatch(t, []string{"format", "version", "review_id", "project_name", "note", "exported_at", "video"}, keysOf(manifest))
	assert.Equal(t, map[string]any{"file_name": "video.mp4", "resolution": "1080p", "aspect_ratio": "9:16", "codec": "h264"}, manifest["video"])

	// Nothing of the rest of the Project, and no key, leaks into what is handed over.
	for name, data := range entries {
		for _, secret := range []string{secretKey, faceMarker, "SECRET-BRIEF-TEXT", otherVideo, o.project, o.assets, "face_reference", "exports/final.mp4"} {
			assert.NotContains(t, string(data), secret, "%s must not contain %q", name, secret)
		}
	}
	assert.NotContains(t, string(pkg), faceMarker, "no reference image is in the archive")
	assert.NotContains(t, string(pkg), secretKey)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestReview_ReviewerAnswersAndOwnerImportsTheResponse(t *testing.T) {
	o := newOwner(t)
	id := o.requestReview("見てください")
	r, inbox := reviewerImports(t, o.packageOf(id))

	// The reviewer sees the video and the owner's note, and has no Project of the owner's.
	page := r.get("/reviews/"+inbox, false)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "見てください")
	assert.Contains(t, page.Body.String(), `src="/reviews/`+inbox+`/video"`)
	video := r.get("/reviews/"+inbox+"/video", false)
	require.Equal(t, http.StatusOK, video.Code)
	assert.Equal(t, mp4Bytes, video.Body.Bytes())
	assert.Equal(t, "video/mp4", video.Header().Get("Content-Type"))
	assert.Equal(t, "sandbox", video.Header().Get("Content-Security-Policy"))
	assert.Equal(t, http.StatusNotFound, r.get("/projects/"+o.project, false).Code, "the reviewer gets no Project")
	var projects, assets int
	require.NoError(t, r.db.Reader().QueryRow(`SELECT (SELECT COUNT(*) FROM project), (SELECT COUNT(*) FROM asset)`).Scan(&projects, &assets))
	assert.Zero(t, projects+assets)
	assert.Contains(t, r.get("/reviews", false).Body.String(), `href="/reviews/`+inbox+`"`)

	// No response before a decision.
	assert.Equal(t, http.StatusConflict, r.get("/reviews/"+inbox+"/response", false).Code)

	rec := r.form(http.MethodPost, "/reviews/"+inbox+"/comments", url.Values{"timecode": {"1:23"}, "body": {"ロゴが小さい"}})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ロゴが小さい")
	assert.Contains(t, rec.Body.String(), "1:23")
	rec = r.form(http.MethodPost, "/reviews/"+inbox+"/comments", url.Values{"body": {"全体に良いです"}})
	require.Equal(t, http.StatusCreated, rec.Code)
	rec = r.form(http.MethodPut, "/reviews/"+inbox+"/decision", url.Values{"reviewer_name": {"Taro"}, "decision": {"changes_requested"}, "comment": {"直してください"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	resp := r.get("/reviews/"+inbox+"/response", false)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Header().Get("Content-Disposition"), "attachment")
	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.ElementsMatch(t, []string{"format", "version", "response_id", "review_id", "reviewer_name", "decision", "comment", "comments", "created_at"}, keysOf(body))
	assert.Equal(t, id, body["review_id"])
	assert.Len(t, body["comments"], 2)

	// The owner imports it: the request now asks for changes and the chat has the result.
	imp := o.importReviewFile("/projects/"+o.project+"/reviews/feedback", "response", "response.json", resp.Body.Bytes())
	require.Equal(t, http.StatusCreated, imp.Code, imp.Body.String())
	assert.Contains(t, imp.Body.String(), "修正依頼あり")
	assert.Contains(t, imp.Body.String(), "ロゴが小さい")
	assert.Contains(t, imp.Body.String(), "Taro")
	var notice string
	require.NoError(t, o.db.Reader().QueryRow(`SELECT content FROM chat_message WHERE project_id = ? AND content LIKE 'レビュー結果%'`, o.project).Scan(&notice))
	assert.Contains(t, notice, "Taro")
	assert.Contains(t, notice, "修正依頼")
	assert.Contains(t, notice, "2 件")

	// The same file is not imported twice.
	again := o.importReviewFile("/projects/"+o.project+"/reviews/feedback", "response", "response.json", resp.Body.Bytes())
	assert.Equal(t, http.StatusConflict, again.Code)
	var n int
	require.NoError(t, o.db.Reader().QueryRow(`SELECT COUNT(*) FROM review_response`).Scan(&n))
	assert.Equal(t, 1, n)

	// Resolving a comment marks it; a comment of another Project is not found.
	var commentID string
	require.NoError(t, o.db.Reader().QueryRow(`SELECT id FROM review_comment WHERE body = 'ロゴが小さい'`).Scan(&commentID))
	res := o.form(http.MethodPut, "/projects/"+o.project+"/review-comments/"+commentID, url.Values{"resolved": {"true"}})
	require.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), "line-through")
	other := o.createProject("別のプロジェクト")
	assert.Equal(t, http.StatusNotFound, o.form(http.MethodPut, "/projects/"+other+"/review-comments/"+commentID, url.Values{"resolved": {"false"}}).Code)

	// Changing the answer makes a new response, which arrives as a further response of the same reviewer.
	rec = r.form(http.MethodPut, "/reviews/"+inbox+"/decision", url.Values{"reviewer_name": {"Taro"}, "decision": {"approved"}})
	require.Equal(t, http.StatusOK, rec.Code)
	resp2 := r.get("/reviews/"+inbox+"/response", false)
	assert.NotEqual(t, resp.Body.String(), resp2.Body.String())
	imp = o.importReviewFile("/projects/"+o.project+"/reviews/feedback", "response", "response2.json", resp2.Body.Bytes())
	require.Equal(t, http.StatusCreated, imp.Code, imp.Body.String())
	assert.Contains(t, imp.Body.String(), "承認済み", "only the latest answer of a reviewer counts")

	// The owner's page lists it with the download link.
	list := o.get("/projects/"+o.project+"/reviews", false)
	require.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), `href="/projects/`+o.project+`/reviews/`+id+`/package"`)
	assert.Contains(t, o.get("/projects/"+o.project, false).Body.String(), `href="/projects/`+o.project+`/reviews"`)
}

func TestReview_OwnerRefusesRequestsAndResponsesThatDoNotBelong(t *testing.T) {
	o := newOwner(t)
	id := o.requestReview("")
	other := o.createProject("別のプロジェクト")

	// A Project cannot make a review of somebody else's Export, or of none.
	for _, exportID := range []string{"", "nope"} {
		rec := o.form(http.MethodPost, "/projects/"+other+"/reviews", url.Values{"export_id": {exportID}})
		assert.Equal(t, http.StatusNotFound, rec.Code, exportID)
	}
	assert.Equal(t, http.StatusNotFound, o.form(http.MethodPost, "/projects/"+other+"/reviews", url.Values{"export_id": {"e1"}}).Code, "an Export of another Project")
	assert.Equal(t, http.StatusUnprocessableEntity, o.form(http.MethodPost, "/projects/"+o.project+"/reviews", url.Values{"export_id": {"e1"}, "note": {strings.Repeat("あ", 1001)}}).Code)
	assert.Equal(t, http.StatusNotFound, o.get("/projects/"+other+"/reviews/"+id+"/package", false).Code, "the package of another Project's request")
	assert.Equal(t, http.StatusNotFound, o.form(http.MethodPost, "/projects/nope/reviews", url.Values{"export_id": {"e1"}}).Code)

	// The Export file is gone.
	require.NoError(t, os.Remove(filepath.Join(o.assets, o.project, "exports", "final.mp4")))
	assert.Equal(t, http.StatusConflict, o.get("/projects/"+o.project+"/reviews/"+id+"/package", false).Code)
	assert.Equal(t, http.StatusConflict, o.form(http.MethodPost, "/projects/"+o.project+"/reviews", url.Values{"export_id": {"e1"}}).Code)

	good := `{"format":"mam-review-response","version":1,"response_id":"r1","review_id":"` + id + `","reviewer_name":"Hana","decision":"approved","comment":"","comments":[],"created_at":"2026-09-30T00:00:00Z"}`
	cases := map[string]string{
		"not json":           `hello`,
		"other format":       strings.Replace(good, "mam-review-response", "other", 1),
		"newer version":      strings.Replace(good, `"version":1`, `"version":2`, 1),
		"unknown field":      strings.Replace(good, `"comment":""`, `"comment":"","api_key":"x"`, 1),
		"bad decision":       strings.Replace(good, `"approved"`, `"maybe"`, 1),
		"empty name":         strings.Replace(good, `"Hana"`, `"  "`, 1),
		"long name":          strings.Replace(good, `"Hana"`, `"`+strings.Repeat("a", 51)+`"`, 1),
		"empty comment body": strings.Replace(good, `"comments":[]`, `"comments":[{"body":" "}]`, 1),
		"negative position":  strings.Replace(good, `"comments":[]`, `"comments":[{"timecode_ms":-1,"body":"x"}]`, 1),
		"too many comments":  strings.Replace(good, `"comments":[]`, `"comments":[`+strings.TrimSuffix(strings.Repeat(`{"body":"x"},`, 201), ",")+`]`, 1),
		"review of nobody":   strings.Replace(good, id, "someone-else", 1),
		"too large":          strings.Replace(good, `"comment":""`, `"comment":"`+strings.Repeat("a", 1<<20)+`"`, 1),
	}
	for name, body := range cases {
		rec := o.importReviewFile("/projects/"+o.project+"/reviews/feedback", "response", "r.json", []byte(body))
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
	}
	// A response for a request of another Project.
	assert.Equal(t, http.StatusUnprocessableEntity, o.importReviewFile("/projects/"+other+"/reviews/feedback", "response", "r.json", []byte(good)).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, o.request(http.MethodPost, "/projects/"+o.project+"/reviews/feedback", nil, "", true).Code, "no file")
	var n int
	require.NoError(t, o.db.Reader().QueryRow(`SELECT COUNT(*) FROM review_response`).Scan(&n))
	assert.Zero(t, n, "every refusal stored nothing")

	assert.Equal(t, http.StatusCreated, o.importReviewFile("/projects/"+o.project+"/reviews/feedback", "response", "r.json", []byte(good)).Code)
}

func TestReview_ReviewerRefusesPackagesThatAreNotOnTheAllowlist(t *testing.T) {
	o := newOwner(t)
	valid := zipEntries(t, o.packageOf(o.requestReview("")))
	manifest := string(valid["manifest.json"])
	pkg := func(entries map[string][]byte) []byte { return zipOf(t, entries) }
	with := func(m map[string][]byte, extra map[string][]byte) map[string][]byte {
		out := map[string][]byte{}
		for k, v := range m {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	webmManifest := strings.Replace(manifest, `"video.mp4"`, `"video.webm"`, 1)
	cases := map[string][]byte{
		"not a zip":         []byte("hello"),
		"extra entry":       pkg(with(valid, map[string][]byte{"data.json": []byte("{}")})),
		"asset entry":       pkg(with(valid, map[string][]byte{"assets/p/face.png": []byte(faceMarker)})),
		"path entry":        pkg(map[string][]byte{"manifest.json": valid["manifest.json"], "../video.mp4": mp4Bytes}),
		"no video":          pkg(map[string][]byte{"manifest.json": valid["manifest.json"]}),
		"no manifest":       pkg(map[string][]byte{"video.mp4": mp4Bytes}),
		"other format":      pkg(map[string][]byte{"manifest.json": []byte(strings.Replace(manifest, "mam-review-package", "x", 1)), "video.mp4": mp4Bytes}),
		"newer version":     pkg(map[string][]byte{"manifest.json": []byte(strings.Replace(manifest, `"version":1`, `"version":2`, 1)), "video.mp4": mp4Bytes}),
		"unknown field":     pkg(map[string][]byte{"manifest.json": []byte(strings.Replace(manifest, `"note"`, `"api_key":"x","note"`, 1)), "video.mp4": mp4Bytes}),
		"long note":         pkg(map[string][]byte{"manifest.json": []byte(strings.Replace(manifest, `"note":""`, `"note":"`+strings.Repeat("a", 1001)+`"`, 1)), "video.mp4": mp4Bytes}),
		"not a video":       pkg(map[string][]byte{"manifest.json": valid["manifest.json"], "video.mp4": []byte("<html>not a video</html>")}),
		"mp4 named as webm": pkg(map[string][]byte{"manifest.json": []byte(webmManifest), "video.webm": mp4Bytes}),
		"names disagree":    pkg(map[string][]byte{"manifest.json": []byte(webmManifest), "video.mp4": mp4Bytes}),
	}
	r := newApp(t)
	for name, body := range cases {
		rec := r.importReviewFile("/reviews/import", "package", "p.zip", body)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
	}
	assert.Equal(t, http.StatusUnprocessableEntity, r.request(http.MethodPost, "/reviews/import", nil, "", true).Code, "no file")
	var n int
	require.NoError(t, r.db.Reader().QueryRow(`SELECT COUNT(*) FROM review_inbox`).Scan(&n))
	assert.Zero(t, n)
	_, err := os.Stat(filepath.Join(r.assets, "reviews"))
	if err == nil {
		entries, _ := os.ReadDir(filepath.Join(r.assets, "reviews"))
		assert.Empty(t, entries, "no video was left behind")
	}

	// A WebM package imports; the same package cannot be imported twice.
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{1}, 32)...)
	ok := pkg(map[string][]byte{"manifest.json": []byte(webmManifest), "video.webm": webm})
	rec := r.importReviewFile("/reviews/import", "package", "p.zip", ok)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusConflict, r.importReviewFile("/reviews/import", "package", "p.zip", ok).Code)
	video := r.get(rec.Header().Get("HX-Redirect")+"/video", false)
	assert.Equal(t, "video/webm", video.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusNotFound, r.get("/reviews/nope", false).Code)
	assert.Equal(t, http.StatusNotFound, r.get("/reviews/nope/video", false).Code)
}

func TestReview_ReviewerInputIsValidated(t *testing.T) {
	o := newOwner(t)
	r, inbox := reviewerImports(t, o.packageOf(o.requestReview("")))
	base := "/reviews/" + inbox

	for name, v := range map[string]url.Values{
		"empty body":       {"body": {" "}},
		"long body":        {"body": {strings.Repeat("あ", 2001)}},
		"bad position":     {"timecode": {"abc"}, "body": {"x"}},
		"negative":         {"timecode": {"-5"}, "body": {"x"}},
		"exponent":         {"timecode": {"1e3"}, "body": {"x"}},
		"more than a day":  {"timecode": {"90000"}, "body": {"x"}},
		"seconds too fine": {"timecode": {"1.2345"}, "body": {"x"}},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, r.form(http.MethodPost, base+"/comments", v).Code, name)
	}
	for name, v := range map[string]url.Values{
		"no name":      {"decision": {"approved"}},
		"blank name":   {"reviewer_name": {"  "}, "decision": {"approved"}},
		"long name":    {"reviewer_name": {strings.Repeat("a", 51)}, "decision": {"approved"}},
		"no decision":  {"reviewer_name": {"Taro"}},
		"bad decision": {"reviewer_name": {"Taro"}, "decision": {"maybe"}},
		"long comment": {"reviewer_name": {"Taro"}, "decision": {"approved"}, "comment": {strings.Repeat("あ", 2001)}},
	} {
		assert.Equal(t, http.StatusUnprocessableEntity, r.form(http.MethodPut, base+"/decision", v).Code, name)
	}
	assert.Equal(t, http.StatusConflict, r.get(base+"/response", false).Code, "refused input wrote no decision")

	// Position forms: seconds, minutes:seconds and none.
	require.Equal(t, http.StatusCreated, r.form(http.MethodPost, base+"/comments", url.Values{"timecode": {"83.5"}, "body": {"a"}}).Code)
	require.Equal(t, http.StatusCreated, r.form(http.MethodPost, base+"/comments", url.Values{"timecode": {"1:23"}, "body": {"b"}}).Code)
	var got []int64
	rows, err := r.db.Reader().Query(`SELECT timecode_ms FROM review_inbox_comment ORDER BY rowid`)
	require.NoError(t, err)
	for rows.Next() {
		var ms int64
		require.NoError(t, rows.Scan(&ms))
		got = append(got, ms)
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, []int64{83500, 83000}, got)

	// A comment is removed only from its own review.
	var cid string
	require.NoError(t, r.db.Reader().QueryRow(`SELECT id FROM review_inbox_comment LIMIT 1`).Scan(&cid))
	assert.Equal(t, http.StatusNotFound, r.form(http.MethodDelete, "/reviews/nope/comments/"+cid, nil).Code)
	assert.Equal(t, http.StatusOK, r.form(http.MethodDelete, base+"/comments/"+cid, nil).Code)
	assert.Equal(t, http.StatusNotFound, r.form(http.MethodDelete, base+"/comments/"+cid, nil).Code)
	assert.Equal(t, http.StatusNotFound, r.form(http.MethodPost, "/reviews/nope/comments", url.Values{"body": {"x"}}).Code)
	assert.Equal(t, http.StatusNotFound, r.form(http.MethodPut, "/reviews/nope/decision", url.Values{"reviewer_name": {"T"}, "decision": {"approved"}}).Code)

	// At most 200 comments.
	r.execSQL(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 198)
		INSERT INTO review_inbox_comment (id, inbox_id, body) SELECT 'fill' || i, ?, 'x' FROM n`, inbox)
	assert.Equal(t, http.StatusCreated, r.form(http.MethodPost, base+"/comments", url.Values{"body": {"the 200th"}}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, r.form(http.MethodPost, base+"/comments", url.Values{"body": {"one too many"}}).Code)
}

func TestReview_ProjectBundleCarriesTheOwnersReviewsButNotTheInbox(t *testing.T) {
	o := newOwner(t)
	id := o.requestReview("")
	r, inbox := reviewerImports(t, o.packageOf(id))
	require.Equal(t, http.StatusOK, r.form(http.MethodPut, "/reviews/"+inbox+"/decision", url.Values{"reviewer_name": {"Taro"}, "decision": {"approved"}}).Code)
	resp := r.get("/reviews/"+inbox+"/response", false)
	require.Equal(t, http.StatusCreated, o.importReviewFile("/projects/"+o.project+"/reviews/feedback", "response", "r.json", resp.Body.Bytes()).Code)

	// Both instances export a bundle: the owner's holds the review, the reviewer's imported package is not a Project.
	bundle := o.get("/projects/"+o.project+"/bundle", false)
	require.Equal(t, http.StatusOK, bundle.Code)
	entries := zipEntries(t, bundle.Body.Bytes())
	assert.Contains(t, string(entries["data.json"]), `"review_request"`)
	assert.Contains(t, string(entries["data.json"]), "Taro")

	dst := newApp(t)
	require.Equal(t, http.StatusCreated, dst.importBundle(bundle.Body.Bytes()).Code)
	var n int
	require.NoError(t, dst.db.Reader().QueryRow(`SELECT COUNT(*) FROM review_response`).Scan(&n))
	assert.Equal(t, 1, n)
	assert.Contains(t, dst.get("/projects/"+o.project+"/reviews", false).Body.String(), "承認済み")
}

func TestReview_ScreensAreOnTheirPages(t *testing.T) {
	o := newOwner(t)
	page := o.get("/projects/"+o.project+"/reviews", false)
	require.Equal(t, http.StatusOK, page.Code)
	body := page.Body.String()
	assert.Contains(t, body, "<html")
	assert.Contains(t, body, `hx-post="/projects/`+o.project+`/reviews"`)
	assert.Contains(t, body, `name="export_id"`)
	assert.Contains(t, body, `hx-post="/projects/`+o.project+`/reviews/feedback"`)
	assert.NotContains(t, o.get("/projects/"+o.project+"/reviews", true).Body.String(), "<html", "HTMX gets the fragment only")
	assert.Equal(t, http.StatusNotFound, o.get("/projects/nope/reviews", false).Code)

	inbox := o.get("/reviews", false)
	require.Equal(t, http.StatusOK, inbox.Code)
	assert.Contains(t, inbox.Body.String(), `hx-post="/reviews/import"`)
	assert.Contains(t, inbox.Body.String(), `name="package"`)
	assert.Contains(t, o.get("/projects", false).Body.String(), `href="/reviews"`)
}
