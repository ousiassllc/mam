package review_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/review"
	"github.com/ousiassllc/mam/backend/internal/service"
)

var mp4Bytes = append([]byte("\x00\x00\x00\x18ftypisom"), bytes.Repeat([]byte{7}, 64)...)

const secretKey = "sk-live-TOPSECRET-API-KEY"

func newEnv(t *testing.T) *mcptest.Env {
	t.Helper()
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { review.Register(r, env.Services) })
}

func exec(t *testing.T, env *mcptest.Env, query string, args ...any) {
	t.Helper()
	require.NoError(t, env.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}))
}

// owner is an instance with a Project that holds two exports and a secret.
type owner struct {
	*mcptest.Env
	projectID string
}

func newOwner(t *testing.T) *owner {
	t.Helper()
	env := newEnv(t)
	require.NoError(t, env.Services.Credentials.Save("kling", secretKey))
	p, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液の縦型CMを作りたい"})
	require.NoError(t, err)
	rel := p.ID + "/exports/final.mp4"
	require.NoError(t, os.MkdirAll(filepath.Join(env.AssetsDir, p.ID, "exports"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.AssetsDir, filepath.FromSlash(rel)), mp4Bytes, 0o644))
	exec(t, env, `INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec) VALUES ('e1', ?, ?, '1080p', '9:16', 'h264')`, p.ID, rel)
	exec(t, env, `INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec) VALUES ('e-gone', ?, ?, '720p', '16:9', 'h264')`, p.ID, p.ID+"/exports/gone.mp4")
	return &owner{Env: env, projectID: p.ID}
}

func (o *owner) request(note string) string {
	o.T.Helper()
	var out review.CreateOutput
	o.CallOK("create_review_request", map[string]any{"project_id": o.projectID, "export_id": "e1", "note": note}, &out)
	require.NotEmpty(o.T, out.ReviewID)
	return out.ReviewID
}

func (o *owner) packageOf(reviewID string) []byte {
	o.T.Helper()
	var link mcpkit.Link
	o.CallOK("get_review_package_url", map[string]any{"project_id": o.projectID, "review_id": reviewID}, &link)
	rec := editortest.Download(o.Env, link.URL)
	require.Equal(o.T, http.StatusOK, rec.Code)
	assert.Equal(o.T, "application/zip", rec.Header().Get("Content-Type"))
	return rec.Body.Bytes()
}

// reviewerImports imports the package into a fresh instance and returns it with the inbox id.
func reviewerImports(t *testing.T, pkg []byte) (*mcptest.Env, string) {
	t.Helper()
	r := newEnv(t)
	rec := editortest.Upload(r, "review_package_import", nil, "review.zip", pkg)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body struct {
		Result struct {
			InboxID string `json:"inbox_id"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body.Result.InboxID)
	return r, body.Result.InboxID
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
		require.NoError(t, rc.Close())
		out[f.Name] = b.Bytes()
	}
	return out
}

func TestListProjectReviews_ShowsRequestsAndTheExportsToRequestFor(t *testing.T) {
	o := newOwner(t)

	var out review.ProjectReviewsOutput
	o.CallOK("list_project_reviews", map[string]any{"project_id": o.projectID}, &out)
	assert.NotNil(t, out.Reviews)
	assert.Empty(t, out.Reviews)
	assert.Len(t, out.Exports, 2)

	id := o.request("最終版です")
	o.CallOK("list_project_reviews", map[string]any{"project_id": o.projectID}, &out)
	require.Len(t, out.Reviews, 1)
	r := out.Reviews[0]
	assert.Equal(t, id, r.ID)
	assert.Equal(t, "pending", r.Status)
	assert.Equal(t, "最終版です", r.Note)
	require.NotNil(t, r.Export)
	assert.Equal(t, "9:16", r.Export.AspectRatio)
	assert.NotNil(t, r.Responses)

	assert.Contains(t, o.CallError("list_project_reviews", map[string]any{"project_id": "nope"}), "not_found")
}

func TestCreateReviewRequest_RefusesWhatTheScreenRefuses(t *testing.T) {
	o := newOwner(t)

	assert.Contains(t, o.CallError("create_review_request", map[string]any{"project_id": o.projectID, "export_id": "nope"}), "not_found")
	assert.Contains(t, o.CallError("create_review_request", map[string]any{"project_id": "nope", "export_id": "e1"}), "not_found")
	msg := o.CallError("create_review_request", map[string]any{"project_id": o.projectID, "export_id": "e-gone"})
	assert.Contains(t, msg, "conflict:", "the file of the export is gone")
	msg = o.CallError("create_review_request", map[string]any{"project_id": o.projectID, "export_id": "e1", "note": strings.Repeat("あ", 1001)})
	assert.Contains(t, msg, "validation:")

	var out review.ProjectReviewsOutput
	o.CallOK("list_project_reviews", map[string]any{"project_id": o.projectID}, &out)
	assert.Empty(t, out.Reviews, "a refused request made nothing")
}

func TestReviewPackage_CarriesTheVideoAndNothingElseOfTheProject(t *testing.T) {
	o := newOwner(t)
	id := o.request("メモ")
	pkg := o.packageOf(id)

	entries := zipEntries(t, pkg)
	require.Len(t, entries, 2, "exactly the manifest and one video")
	assert.Equal(t, mp4Bytes, entries["video.mp4"])
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(entries["manifest.json"], &manifest))
	assert.Equal(t, id, manifest["review_id"])
	assert.Equal(t, "メモ", manifest["note"])
	for name, data := range entries {
		for _, secret := range []string{secretKey, o.projectID, o.AssetsDir, "exports/final.mp4"} {
			assert.NotContains(t, string(data), secret, "%s must not hold %q", name, secret)
		}
	}

	assert.Contains(t, o.CallError("get_review_package_url", map[string]any{"project_id": o.projectID, "review_id": "nope"}), "not_found")
	assert.Contains(t, o.CallError("get_review_package_url", map[string]any{"project_id": "other", "review_id": id}), "not_found", "the request of another Project")
}

func TestReviewRoundTrip_ReviewerCommentsDecidesAndTheOwnerImportsTheResponse(t *testing.T) {
	o := newOwner(t)
	reviewID := o.request("お願いします")
	rv, inboxID := reviewerImports(t, o.packageOf(reviewID))

	var inbox review.InboxOutput
	rv.CallOK("list_review_inbox", map[string]any{}, &inbox)
	require.Len(t, inbox.Reviews, 1)
	assert.Equal(t, inboxID, inbox.Reviews[0].ID)
	assert.Equal(t, reviewID, inbox.Reviews[0].ReviewID)
	assert.False(t, inbox.Reviews[0].CanRespond, "no decision yet")

	msg := rv.CallError("get_review_response_url", map[string]any{"inbox_id": inboxID})
	assert.Contains(t, msg, "conflict:", "the response is written after a decision")

	var item review.InboxItem
	rv.CallOK("add_review_comment", map[string]any{"inbox_id": inboxID, "position": "1:23", "body": "ここが速い"}, &item)
	rv.CallOK("add_review_comment", map[string]any{"inbox_id": inboxID, "body": "全体にもう少し明るく"}, &item)
	require.Len(t, item.Comments, 2)
	require.NotNil(t, item.Comments[0].TimecodeMs)
	assert.EqualValues(t, 83000, *item.Comments[0].TimecodeMs)
	assert.Equal(t, "1:23", item.Comments[0].Timecode)
	assert.Nil(t, item.Comments[1].TimecodeMs)
	rv.CallOK("delete_review_comment", map[string]any{"inbox_id": inboxID, "comment_id": item.Comments[1].ID}, &item)
	require.Len(t, item.Comments, 1)

	rv.CallOK("decide_review", map[string]any{"inbox_id": inboxID, "reviewer_name": "高橋", "decision": "changes_requested", "comment": "直してください"}, &item)
	assert.True(t, item.CanRespond)
	assert.Equal(t, "changes_requested", item.Decision)

	var video, resp mcpkit.Link
	rv.CallOK("get_review_video_url", map[string]any{"inbox_id": inboxID}, &video)
	rec := editortest.Download(rv, video.URL)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, mp4Bytes, rec.Body.Bytes())
	assert.Equal(t, "video/mp4", rec.Header().Get("Content-Type"))

	rv.CallOK("get_review_response_url", map[string]any{"inbox_id": inboxID}, &resp)
	rec = editortest.Download(rv, resp.URL)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, resp.FileName, "mam-review-response-")

	// Back on the owner's instance.
	up := editortest.Upload(o.Env, "review_response", map[string]string{"project_id": o.projectID}, resp.FileName, rec.Body.Bytes())
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())
	var out review.ProjectReviewsOutput
	o.CallOK("list_project_reviews", map[string]any{"project_id": o.projectID}, &out)
	require.Len(t, out.Reviews, 1)
	r := out.Reviews[0]
	assert.Equal(t, "changes_requested", r.Status)
	assert.Equal(t, 1, r.OpenComments)
	require.Len(t, r.Responses, 1)
	assert.Equal(t, "高橋", r.Responses[0].ReviewerName)
	require.Len(t, r.Responses[0].Comments, 1)
	assert.Equal(t, "ここが速い", r.Responses[0].Comments[0].Body)

	// The same response again is refused, and imports nothing.
	again := editortest.Upload(o.Env, "review_response", map[string]string{"project_id": o.projectID}, resp.FileName, rec.Body.Bytes())
	assert.Equal(t, http.StatusConflict, again.Code)

	commentID := r.Responses[0].Comments[0].ID
	var resolved review.RequestItem
	o.CallOK("set_review_comment_resolved", map[string]any{"project_id": o.projectID, "comment_id": commentID, "resolved": true}, &resolved)
	assert.Equal(t, 0, resolved.OpenComments)
	assert.True(t, resolved.Responses[0].Comments[0].Resolved)
	o.CallOK("set_review_comment_resolved", map[string]any{"project_id": o.projectID, "comment_id": commentID, "resolved": false}, &resolved)
	assert.Equal(t, 1, resolved.OpenComments)
	assert.Contains(t, o.CallError("set_review_comment_resolved", map[string]any{"project_id": o.projectID, "comment_id": "nope", "resolved": true}), "not_found")
}

func TestReviewerTools_RefuseWhatTheScreenRefuses(t *testing.T) {
	o := newOwner(t)
	rv, inboxID := reviewerImports(t, o.packageOf(o.request("")))

	assert.Contains(t, rv.CallError("get_review", map[string]any{"inbox_id": "nope"}), "not_found")
	assert.Contains(t, rv.CallError("add_review_comment", map[string]any{"inbox_id": "nope", "body": "x"}), "not_found")
	assert.Contains(t, rv.CallError("add_review_comment", map[string]any{"inbox_id": inboxID, "position": "あ", "body": "x"}), "validation:")
	assert.Contains(t, rv.CallError("add_review_comment", map[string]any{"inbox_id": inboxID, "body": "   "}), "validation:")
	assert.Contains(t, rv.CallError("add_review_comment", map[string]any{"inbox_id": inboxID, "body": strings.Repeat("あ", 2001)}), "validation:")
	assert.Contains(t, rv.CallError("delete_review_comment", map[string]any{"inbox_id": inboxID, "comment_id": "nope"}), "not_found")
	assert.Contains(t, rv.CallError("decide_review", map[string]any{"inbox_id": inboxID, "reviewer_name": "", "decision": "approved"}), "validation:")
	assert.Contains(t, rv.CallError("decide_review", map[string]any{"inbox_id": inboxID, "reviewer_name": "高橋", "decision": "maybe"}), "validation:")
	assert.Contains(t, rv.CallError("decide_review", map[string]any{"inbox_id": "nope", "reviewer_name": "高橋", "decision": "approved"}), "not_found")
	assert.Contains(t, rv.CallError("get_review_video_url", map[string]any{"inbox_id": "nope"}), "not_found")

	var item review.InboxItem
	rv.CallOK("get_review", map[string]any{"inbox_id": inboxID}, &item)
	assert.Empty(t, item.Decision, "no refused call wrote a decision")
	assert.Empty(t, item.Comments)
}

func TestReviewImports_RefuseWhatTheScreenRefuses(t *testing.T) {
	o := newOwner(t)
	pkg := o.packageOf(o.request(""))
	rv, _ := reviewerImports(t, pkg)

	assert.Equal(t, http.StatusConflict, editortest.Upload(rv, "review_package_import", nil, "r.zip", pkg).Code, "the same package twice")
	assert.Equal(t, http.StatusUnprocessableEntity, editortest.Upload(rv, "review_package_import", nil, "r.zip", []byte("not a zip")).Code)

	var junk bytes.Buffer
	zw := zip.NewWriter(&junk)
	w, err := zw.Create("manifest.json")
	require.NoError(t, err)
	_, err = w.Write([]byte(`{}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	assert.Equal(t, http.StatusUnprocessableEntity, editortest.Upload(newEnv(t), "review_package_import", nil, "r.zip", junk.Bytes()).Code, "a package that is not a manifest and one video")

	for name, body := range map[string][]byte{"not json": []byte("x"), "wrong format": []byte(`{"format":"other"}`), "unknown field": []byte(`{"format":"mam-review-response","evil":1}`)} {
		rec := editortest.Upload(o.Env, "review_response", map[string]string{"project_id": o.projectID}, "r.json", body)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
	}
	assert.Equal(t, http.StatusNotFound, editortest.Upload(o.Env, "review_response", map[string]string{"project_id": "nope"}, "r.json", []byte(`{}`)).Code)

	var out review.ProjectReviewsOutput
	o.CallOK("list_project_reviews", map[string]any{"project_id": o.projectID}, &out)
	for _, r := range out.Reviews {
		assert.Empty(t, r.Responses, "no refused response was imported")
	}
}
