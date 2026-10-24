package modelassign

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelassignment"
)

// SectionData is the saved assignment as the section shows it (the page handler of the settings screen
// calls it). It is the saved assignment, which the app has applied (SetOnChange). A settings file that cannot be
// read is the Problem of the section, so that the rest of the screen still works.
func SectionData(svc *modelsettings.Service) modelassignment.Data {
	return sectionData(svc, nil)
}

// sectionData is SectionData; submitted, when not nil, is shown instead of the saved routes.
func sectionData(svc *modelsettings.Service, submitted *modelsettings.Input) modelassignment.Data {
	v, err := svc.View()
	if err != nil {
		var invalid *modelsettings.InvalidError
		if errors.As(err, &invalid) {
			return modelassignment.Data{Problem: invalid.Message}
		}
		return modelassignment.Data{Problem: unavailable}
	}
	return Data(v, submitted)
}

// Save stores the assignment of the form (PUT /settings/models; fields named by modelassignment's
// ImageModelField, ImageFastField, PolicyTargetField and PolicyTakesField) and answers with the section:
// 200 with the notice, or 422 with the reason and what was submitted when it cannot be saved (the
// file is then untouched).
func Save(svc *modelsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = c.Request.ParseForm()
		in := ParseInput(c.Request.PostForm)
		answer(c, svc, svc.Save(in), savedNotice, &in)
	}
}

// Reset drops the saved image / video assignment (DELETE /settings/models), so that the defaults apply
// at once, and answers with the section.
func Reset(svc *modelsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		answer(c, svc, svc.Reset(), resetNotice, nil)
	}
}

// answer renders the section after an operation. submitted is what the form held: shown again when the
// operation failed, so that a mistake in one field does not discard the rest.
func answer(c *gin.Context, svc *modelsettings.Service, err error, notice string, submitted *modelsettings.Input) {
	if err == nil {
		d := SectionData(svc)
		d.Notice = notice
		render(c, http.StatusOK, modelassignment.Section(d))
		return
	}
	status, message := http.StatusInternalServerError, "設定ファイルを書き込めませんでした。保存先の権限と空き容量を確認してください"
	var invalid *modelsettings.InvalidError
	switch {
	case errors.Is(err, modelsettings.ErrPinned):
		status, message, submitted = http.StatusConflict, "起動オプションで固定されているため、この画面では変更できません", nil
	case errors.As(err, &invalid):
		status, message = http.StatusUnprocessableEntity, invalid.Message
	default:
		_ = c.Error(err)
	}
	d := sectionData(svc, submitted)
	if d.Problem == "" {
		d.Error = message
	}
	render(c, status, modelassignment.Section(d))
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
