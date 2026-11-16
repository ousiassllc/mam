package gentest

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/audioinput"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The rows of the pickers: an image of the history or of a Project, and (Issue #1153) an audio file of a Project.

func historyRef(r record.ResultView) view.Ref {
	v := refform.Value{Source: gentest.FromHistory, ID: r.ID}
	return view.Ref{
		Value: v.String(), Label: r.ModelLabel + "・" + shorten(r.Prompt, 40), Role: gentest.RoleReference,
		ThumbURL: paths.GenerationTestPart("results", r.ID, "file"),
	}
}

func projectRef(projectID string, im projectImage) view.Ref {
	source, id := gentest.FromProjectAsset, im.AssetID
	label := "アセット " + shorten(im.AssetID, 8)
	if im.ReferenceItemID != "" {
		source, id, label = gentest.FromReferenceItem, im.ReferenceItemID, im.Name+"（参照）"
	}
	return view.Ref{
		Value: refform.Value{Source: source, ProjectID: projectID, ID: id}.String(), Label: label, Role: gentest.RoleReference,
		ThumbURL: paths.Project(projectID, "assets", im.AssetID),
	}
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// projectImage is a Project image as the service lists it.
type projectImage = projectlink.ProjectImage

// audioLabel names an audio asset of a Project in a list: its short id and its size.
func audioLabel(a projectlink.ProjectAudio) string {
	return "音声 " + shorten(a.AssetID, 8) + "（" + sizeText(a.SizeBytes) + "）"
}

// projectAudioRef is the row of an audio asset of a Project: its player is the file of the asset.
func projectAudioRef(projectID string, a projectlink.ProjectAudio) view.Ref {
	return view.Ref{
		Value: refform.Value{Source: gentest.FromProjectAsset, ProjectID: projectID, ID: a.AssetID}.String(), Label: audioLabel(a), Role: gentest.RoleAudio,
		ThumbURL: paths.Project(projectID, "assets", a.AssetID),
	}
}

// projectAudioRow is the row of the audio asset the form names; false for one that is not there any more (or is no audio asset of that Project).
func (h Handlers) projectAudioRow(ctx context.Context, r refform.Value) (view.Ref, bool) {
	list, err := h.Svc.ProjectAudios(ctx, r.ProjectID)
	if err != nil {
		return view.Ref{}, false
	}
	for _, a := range list {
		if a.AssetID == r.ID {
			return projectAudioRef(r.ProjectID, a), true
		}
	}
	return view.Ref{}, false
}

// audioPicker answers GET /generation-test/picker?kind=audio&audio_project=<id>: the audio files of the Project, each with a player and the button that adds it
// to the inputs as the sound of the batch.
func (h Handlers) audioPicker(c *gin.Context) {
	rows := audioinput.Rows{
		URL: paths.GenerationTestPart("ref-row"), Target: "#" + view.RefsID,
		Include: "#" + view.ComposeID + " [name=media], #" + view.ComposeID + " [name=model]",
	}
	project := c.Query(audioinput.ProjectField)
	if project == "" {
		gtshare.Render(c, http.StatusOK, audioinput.Picks(nil, "プロジェクトを選ぶと、その音声ファイルが並びます", rows))
		return
	}
	list, err := h.Svc.ProjectAudios(c.Request.Context(), project)
	if err != nil {
		status, msg := refuse(c, err)
		gtshare.Render(c, status, audioinput.Picks(nil, msg, rows))
		return
	}
	picks := make([]audioinput.Pick, len(list))
	for i, a := range list {
		ref := projectAudioRef(project, a)
		picks[i] = audioinput.Pick{Value: ref.Value, Label: ref.Label, URL: ref.ThumbURL}
	}
	gtshare.Render(c, http.StatusOK, audioinput.Picks(picks, "このプロジェクトに音声ファイルがありません（チャットに添付すると、ここに並びます）", rows))
}
