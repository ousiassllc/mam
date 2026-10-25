package media

import (
	"errors"
	"net/http"
	"slices"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const (
	pinnedReason = "起動オプションで固定されているため、この画面では変更できません"
	writeFailed  = "設定ファイルを書き込めませんでした。保存先の権限と空き容量を確認してください"
	notFound     = "対象が見つかりません"
	imageTitle   = "画像モデル"
	policyTitle  = "動画モデル"
)

// SectionData is the saved assignment as the category shows it (the page handler of the settings screen calls it). It is the
// saved assignment, which the app has applied (SetOnChange). A settings file that cannot be read is the Problem of the
// category, so that the rest of the screen still works.
func SectionData(svc *modelsettings.Service) media.Data {
	v, err := svc.View()
	if err != nil {
		return media.Data{Problem: problem(err)}
	}
	return Data(v)
}

// problem is why the settings file cannot be used, as the category says it.
func problem(err error) string {
	var invalid *modelsettings.InvalidError
	if errors.As(err, &invalid) {
		return invalid.Message
	}
	return unavailable
}

// Save stores the whole assignment of the form (PUT /settings/models; fields named by media's ImageModelField,
// ImageFastField, PolicyTargetField and PolicyTakesField) and answers with the category: 200 with the notice, or 422 with
// the reason when it cannot be saved (the file is then untouched). The screen saves one row at a time (SaveImage,
// SavePolicy); this is for the whole saves of the tests.
func Save(svc *modelsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = c.Request.ParseForm()
		answer(c, svc, svc.Save(ParseInput(c.Request.PostForm)), savedNotice)
	}
}

// Reset drops the saved image / video assignment (DELETE /settings/models), so that the defaults apply at once, and answers
// with the category.
func Reset(svc *modelsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		answer(c, svc, svc.Reset(), resetNotice)
	}
}

// answer renders the category after an operation on the whole assignment.
func answer(c *gin.Context, svc *modelsettings.Service, err error, notice string) {
	status, message := http.StatusOK, ""
	var invalid *modelsettings.InvalidError
	switch {
	case err == nil:
	case errors.Is(err, modelsettings.ErrPinned):
		status, message = http.StatusConflict, pinnedReason
	case errors.As(err, &invalid):
		status, message = http.StatusUnprocessableEntity, invalid.Message
	default:
		_ = c.Error(err)
		status, message = http.StatusInternalServerError, writeFailed
	}
	d := SectionData(svc)
	if d.Problem == "" {
		d.Notice, d.Error = notice, message
		if message != "" {
			d.Notice = ""
		}
	}
	settingshttp.Render(c, status, media.Section(d))
}

// rowRequest is what a request about one row (an image purpose or a Quality Policy) needs: the saved assignment, and the
// refusal when the row cannot be edited (404, 409). ok is false when it answered.
func rowRequest(c *gin.Context, svc *modelsettings.Service, known bool, title string) (v modelsettings.View, ok bool) {
	if !known {
		settingshttp.Render(c, http.StatusNotFound, modal.Notice(title, notFound))
		return v, false
	}
	v, err := svc.View()
	switch {
	case err != nil:
		settingshttp.Render(c, http.StatusConflict, modal.Notice(title, problem(err)))
	case v.Pinned:
		settingshttp.Render(c, http.StatusConflict, modal.Notice(title, pinnedReason))
	default:
		return v, true
	}
	return v, false
}

func imagePurpose(c *gin.Context) (modelsettings.ImagePurpose, bool) {
	p := modelsettings.ImagePurpose(c.Param("purpose"))
	return p, slices.Contains(modelsettings.ImagePurposes(), p)
}

func qualityPolicy(c *gin.Context) (domain.QualityPolicy, bool) {
	q := domain.QualityPolicy(c.Param("policy"))
	return q, slices.Contains(domain.QualityPolicies(), q)
}

// EditImage answers the modal that edits the image purpose :purpose (GET /settings/models/images/:purpose/edit), with the
// models that may be chosen now. An unknown purpose is 404 and a pinned file 409, each with a modal that says so. A request
// that is not HTMX (the address typed in, a reload) gets page, the category page.
func EditImage(svc *modelsettings.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		p, known := imagePurpose(c)
		v, ok := rowRequest(c, svc, known, imageTitle)
		if !ok {
			return
		}
		settingshttp.Render(c, http.StatusOK, media.ImageModal(media.ImageForm{Row: imageRow(v, p, v.Images[p]), Models: imageModels(v)}))
	}
}

// EditPolicy is EditImage for the Quality Policy :policy (GET /settings/models/policies/:policy/edit).
func EditPolicy(svc *modelsettings.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		q, known := qualityPolicy(c)
		v, ok := rowRequest(c, svc, known, policyTitle)
		if !ok {
			return
		}
		settingshttp.Render(c, http.StatusOK, media.PolicyModal(media.PolicyForm{Row: policyRow(v, q, v.Policies[q]), Choices: videoChoices(v)}))
	}
}

// SaveImage stores the models of the image purpose :purpose (PUT /settings/models/images/:purpose, the fields of that
// purpose) for the modal that sent it. The row is checked on its own: the others stay as saved. The answer is the row
// replaced out of band, the notes and the reset (which a save may change), a toast and the event that closes the modal; 422
// is the modal again with why and what was typed (the file is then untouched), 409 a pinned file, 404 an unknown purpose.
func SaveImage(svc *modelsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, known := imagePurpose(c)
		if _, ok := rowRequest(c, svc, known, imageTitle); !ok {
			return
		}
		_ = c.Request.ParseForm()
		route := parseImage(c.Request.PostForm, p)
		err := svc.SaveImage(p, route)
		v, viewErr := svc.View()
		if viewErr != nil {
			refuse(c, errors.Join(err, viewErr), nil)
			return
		}
		if err != nil {
			refuse(c, err, func(msg, field string) templ.Component {
				return media.ImageModal(media.ImageForm{Row: imageRow(v, p, route), Models: imageModels(v), Error: msg, Blame: field})
			})
			return
		}
		saved := imageRow(v, p, v.Images[p])
		done(c, v, media.ImageRowOOB(saved), saved.Label+" の画像モデルを保存しました。すぐに反映されます")
	}
}

// SavePolicy is SaveImage for the Quality Policy :policy (PUT /settings/models/policies/:policy).
func SavePolicy(svc *modelsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		q, known := qualityPolicy(c)
		if _, ok := rowRequest(c, svc, known, policyTitle); !ok {
			return
		}
		_ = c.Request.ParseForm()
		route := parsePolicy(c.Request.PostForm, q)
		err := svc.SavePolicy(q, route)
		v, viewErr := svc.View()
		if viewErr != nil {
			refuse(c, errors.Join(err, viewErr), nil)
			return
		}
		if err != nil {
			refuse(c, err, func(msg, field string) templ.Component {
				return media.PolicyModal(media.PolicyForm{Row: policyRow(v, q, route), Choices: videoChoices(v), Error: msg, Blame: field})
			})
			return
		}
		saved := policyRow(v, q, v.Policies[q])
		done(c, v, media.PolicyRowOOB(saved), saved.Label+" の動画モデルを保存しました。すぐに反映されます")
	}
}

// done answers a save of a row that worked: the row first (a <tr> is read as a row only at the head of the answer), then the
// notes and the reset, which the save may have changed, and the toast.
func done(c *gin.Context, v modelsettings.View, row templ.Component, toast string) {
	settingshttp.CloseModal(c)
	settingshttp.Render(c, http.StatusOK, templ.Join(row, media.Status(Data(v), true), atoms.SuccessToastOOB(toast)))
}

// refuse answers a save that did not happen with the modal again (draw) and why: 422 for a refused input, 409 for a file
// that was pinned or unreadable meanwhile, 500 for a failed write. draw is nil when the saved assignment cannot be read
// any more: the modal then says only that.
func refuse(c *gin.Context, err error, draw func(message, field string) templ.Component) {
	status, message, field := http.StatusInternalServerError, writeFailed, ""
	var invalid *modelsettings.InvalidError
	switch {
	case errors.Is(err, modelsettings.ErrPinned):
		status, message = http.StatusConflict, pinnedReason
	case errors.As(err, &invalid):
		status, message, field = http.StatusUnprocessableEntity, invalid.Message, invalid.Field
	default:
		_ = c.Error(err)
	}
	if draw == nil || status == http.StatusConflict {
		settingshttp.Render(c, status, modal.Notice("画像・動画モデル", message))
		return
	}
	settingshttp.Render(c, status, draw(message, field))
}

// Register adds the routes of the category: the modals (GET paths.ModelImageEdit, paths.ModelPolicyEdit), the saves of the
// rows (PUT paths.ModelImage, paths.ModelPolicy) and the save and reset of the whole assignment (PUT / DELETE
// paths.ModelSettings). page answers a modal route for a request that is not HTMX.
func Register(engine *gin.Engine, svc *modelsettings.Service, page gin.HandlerFunc) {
	engine.GET(paths.ModelImageEdit(":purpose"), EditImage(svc, page))
	engine.GET(paths.ModelPolicyEdit(":policy"), EditPolicy(svc, page))
	engine.PUT(paths.ModelImage(":purpose"), SaveImage(svc))
	engine.PUT(paths.ModelPolicy(":policy"), SavePolicy(svc))
	engine.PUT(paths.ModelSettings(), Save(svc))
	engine.DELETE(paths.ModelSettings(), Reset(svc))
}
