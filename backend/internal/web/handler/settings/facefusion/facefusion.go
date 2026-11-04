// Package facefusion is the handlers of the "FaceFusion" category of the settings screen (spec/architecture/gentest/face-swap.md §2): the page
// body and the save of the Python and the FaceFusion folder (PUT /settings/facefusion).
package facefusion

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/failure"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/facefusion"
)

const (
	unreadable    = "設定ファイル（facefusion.json）を読めません。壊れていれば、直すか削除してください"
	savedNotice   = "保存しました。次の顔の入れ替えから使われます"
	clearedNotice = "パスの指定を解除しました。次の顔の入れ替えから、自動で探します"
)

// SectionData is the saved paths, what the lookup finds with them, the models with their licenses and the standing cautions. A settings file
// that cannot be read is the Problem of the category, so that the rest of the screen still works.
func SectionData(s *facefusionsettings.Service) view.Data {
	v, err := s.View()
	if err != nil {
		return view.Data{Problem: unreadable}
	}
	d := base()
	d.Python = part(v.Python, "Python のパス（FaceFusion の環境のもの）", view.FieldPython, "空: conda 環境 facefusion の python、なければ PATH の python3 / python")
	d.Path = part(v.Path, "FaceFusion のフォルダ（facefusion.py があるところ）", view.FieldPath, "空: 自動で探す")
	d.Ready, d.Version, d.License, d.SearchDirs = v.Ready(), v.Version, v.License, v.SearchDirs
	return d
}

// base is what does not depend on the installation: the models and the cautions.
func base() view.Data {
	d := view.Data{
		LicensesURL: facefusion.LicensesURL, DisclaimerURL: facefusion.DisclaimerURL,
		Notes: []string{facefusion.LicenseNote, facefusion.ArcFaceNote, facefusion.AdultNote, facefusion.FirstRunNote},
	}
	d.Groups = []view.ModelGroup{
		group("入れ替えモデル", facefusion.Swappers, facefusion.DefaultSwapper),
		group("顔検出モデル", facefusion.Detectors, facefusion.DefaultDetector),
		group("顔の復元モデル", facefusion.Enhancers, facefusion.DefaultEnhancer),
	}
	return d
}

func group(title string, models []facefusion.Model, def string) view.ModelGroup {
	g := view.ModelGroup{Title: title}
	for _, m := range models {
		g.Models = append(g.Models, view.ModelRow{
			ID: m.ID, Label: m.Label, License: m.License, Class: m.Class.Text(), Tone: tone(m.Class), Default: m.ID == def, Note: m.Note,
		})
	}
	return g
}

func tone(c facefusion.Class) atoms.BadgeTone {
	switch c {
	case facefusion.ClassOpen:
		return atoms.BadgeOK
	case facefusion.ClassConditional:
		return atoms.BadgeInfo
	case facefusion.ClassUnknown:
		return atoms.BadgeNeutral
	}
	return atoms.BadgeWarn
}

func part(p facefusionsettings.Part, label, field, placeholder string) view.Part {
	out := view.Part{Label: label, Field: field, Configured: p.Configured, Found: p.Found, Placeholder: placeholder}
	var cfgErr *facefusion.ConfigError
	switch {
	case p.Err == nil:
	case errors.As(p.Err, &cfgErr):
		out.Problem = "指定したパスは使えません: " + reason(cfgErr)
	default:
		out.Problem = "探した場所のどこにもありません"
	}
	return out
}

// Save sets the paths of the form (PUT /settings/facefusion, fields python and path; an empty one clears the path) and answers with the
// category: 200 with the notice, or 422 with the reason and what was typed when a path cannot be used (nothing is then written).
func Save(s *facefusionsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := facefusion.Config{Python: c.PostForm(view.FieldPython), Path: c.PostForm(view.FieldPath)}
		err := s.Set(cfg)
		d := SectionData(s)
		status := http.StatusOK
		var cfgErr *facefusion.ConfigError
		switch {
		case err == nil && cfg.Python == "" && cfg.Path == "":
			d.Notice = clearedNotice
		case err == nil:
			d.Notice = savedNotice
		case errors.As(err, &cfgErr):
			status = http.StatusUnprocessableEntity
			d.Error = fieldLabel(cfgErr.Field) + "（" + cfgErr.Path + "）を保存できませんでした。" + reason(cfgErr)
			d.Python.Configured, d.Path.Configured = cfg.Python, cfg.Path
		default:
			_ = c.Error(err)
			status, d.Error = failure.Save(err)
		}
		settingshttp.Render(c, status, view.Section(d))
	}
}

func fieldLabel(field string) string {
	if field == facefusion.FieldPython {
		return "Python のパス"
	}
	return "FaceFusion のフォルダ"
}

// reason is why a path was refused, for the person who typed it.
func reason(e *facefusion.ConfigError) string {
	switch {
	case errors.Is(e, locate.ErrNotAbsolute):
		return "絶対パスではありません。/ や C:\\ から始まるパスを指定してください"
	case errors.Is(e, locate.ErrNotExecutable):
		return "実在する実行ファイルではありません。ファイルの場所と、実行できる権限を確認してください"
	case errors.Is(e, facefusion.ErrNoScript):
		return "そのフォルダに facefusion.py がありません。FaceFusion を取得したフォルダを指定してください"
	case errors.Is(e, facefusion.ErrNotFaceFusion):
		return "FaceFusion のフォルダではありません（facefusion/metadata.py がありません）"
	}
	return e.Err.Error()
}
