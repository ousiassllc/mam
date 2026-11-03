// Package voicetrainhandler holds the handlers of the voice training panel of a Character (Issue #936): the panel itself, a clip, the start, the
// cancel and the removal of the trained voice, for the canonical Characters of a Series and for the Characters of a Project. Every answer is the
// panel alone. It is a package of its own because handler is at its size limit.
package voicetrainhandler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/service/voicetrain"
	"github.com/ousiassllc/mam/backend/internal/web/handler/failure"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/voicetrainpanel"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// A clip is read like a reference voice: the body is the clip's own limit and the form fields around it.
const (
	multipartMemory = 8 << 20
	bodyLimit       = voicetrain.MaxClipBytes + 1<<20
)

var errTooLarge = errors.New("voicetrainhandler: request too large")

// Target is whose Character the route is of.
type Target struct {
	series bool
}

// Series is the target of the routes of a Series' canonical Characters (/series/:id/characters/:characterId/voice-training).
var Series = Target{series: true}

// Project is the target of the routes of a Project's Characters (/projects/:id/characters/:characterId/voice-training).
var Project = Target{}

func (t Target) owner(c *gin.Context) voicerefsvc.Owner {
	if t.series {
		return voicerefsvc.ForSeries(c.Param("id"))
	}
	return voicerefsvc.ForProject(c.Param("id"))
}

// url is the route of the panel of the Character the request is for.
func (t Target) url(c *gin.Context) string {
	if t.series {
		return paths.Series(c.Param("id"), "characters", c.Param("characterId"), "voice-training")
	}
	return paths.Project(c.Param("id"), "characters", c.Param("characterId"), "voice-training")
}

// DOMID is the id of the panel of a Character.
func DOMID(characterID string) string { return "voice-train-" + characterID }

func (t Target) panel(c *gin.Context, svc *service.Services, status int) {
	o, id := t.owner(c), c.Param("characterId")
	sum, err := svc.VoiceTrain.Summarize(c.Request.Context(), o, id)
	if err != nil {
		fail(c, err)
		return
	}
	name, err := svc.VoiceRef.CharacterName(c.Request.Context(), o, id)
	if err != nil {
		fail(c, err)
		return
	}
	settingshttp.NoStore(c)
	settingshttp.Render(c, status, voicetrainpanel.Panel(panelData(DOMID(id), t.url(c), name, sum)))
}

// Panel answers the panel (GET .../voice-training).
func (t Target) Panel(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) { t.panel(c, svc, http.StatusOK) }
}

// Start starts the training (POST .../voice-training; epochs_sovits and epochs_gpt, left out: the defaults) and answers the panel of the running one.
func (t Target) Start(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		opt, err := epochs(c)
		if err == nil {
			_, err = svc.VoiceTrain.Start(c.Request.Context(), t.owner(c), c.Param("characterId"), opt)
		}
		if err != nil {
			fail(c, err)
			return
		}
		t.panel(c, svc, http.StatusOK)
	}
}

func epochs(c *gin.Context) (voicetrain.Options, error) {
	var opt voicetrain.Options
	for name, to := range map[string]*int{"epochs_sovits": &opt.EpochsSoVITS, "epochs_gpt": &opt.EpochsGPT} {
		v := strings.TrimSpace(c.PostForm(name))
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return opt, &apperr.Error{Code: apperr.CodeInvalidInput, Cause: "エポック数は整数で入力してください。"}
		}
		*to = n
	}
	return opt, nil
}

// Cancel stops the training that runs for the Character (POST .../voice-training/cancel) and answers the panel.
func (t Target) Cancel(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.VoiceTrain.Cancel(t.owner(c), c.Param("characterId")); err != nil {
			fail(c, err)
			return
		}
		t.panel(c, svc, http.StatusOK)
	}
}

// AddClip adds a clip (POST .../voice-training/clips, multipart file and text) and answers the panel.
func (t Target) AddClip(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, text, err := readClip(c)
		if err == nil {
			_, err = svc.VoiceTrain.AddClip(c.Request.Context(), t.owner(c), c.Param("characterId"), file, text)
		}
		if err != nil {
			fail(c, err)
			return
		}
		t.panel(c, svc, http.StatusOK)
	}
}

// RemoveClip removes one clip (DELETE .../voice-training/clips/:clipId) and RemoveClips all of them (DELETE .../voice-training/clips).
func (t Target) RemoveClip(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.VoiceTrain.RemoveClips(c.Request.Context(), t.owner(c), c.Param("characterId"), c.Param("clipId")); err != nil {
			fail(c, err)
			return
		}
		t.panel(c, svc, http.StatusOK)
	}
}

// RemoveTrained removes the trained voice (DELETE .../voice-training/voice): the Character speaks with the zero-shot clone again.
func (t Target) RemoveTrained(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.VoiceTrain.RemoveTrained(c.Request.Context(), t.owner(c), c.Param("characterId")); err != nil {
			fail(c, err)
			return
		}
		t.panel(c, svc, http.StatusOK)
	}
}

func readClip(c *gin.Context) (voicerefsvc.File, string, error) {
	if c.Request.ContentLength > bodyLimit {
		return voicerefsvc.File{}, "", errTooLarge
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimit)
	if err := c.Request.ParseMultipartForm(multipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return voicerefsvc.File{}, "", errTooLarge
		}
		return voicerefsvc.File{}, "", &voicerefsvc.InvalidError{Message: "送信内容を読み取れませんでした（multipart/form-data で送ってください）"}
	}
	form := c.Request.MultipartForm
	var file voicerefsvc.File
	if files := form.File["file"]; len(files) > 0 && files[0].Filename != "" {
		file = uploadOf(files[0])
	}
	text := ""
	if v := form.Value["text"]; len(v) > 0 {
		text = v[0]
	}
	return file, text, nil
}

func uploadOf(fh *multipart.FileHeader) voicerefsvc.File {
	return voicerefsvc.File{Name: fh.Filename, Size: fh.Size, Open: func() (io.ReadCloser, error) { return fh.Open() }}
}

// statusOf maps an error to the status and the message the user sees: the refusals of the training are the common error (#1010).
func statusOf(c *gin.Context, err error) (int, string) {
	var invalid *voicerefsvc.InvalidError
	switch {
	case errors.Is(err, errTooLarge):
		return http.StatusRequestEntityTooLarge, "送信内容が大きすぎます"
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.Is(err, voicerefsvc.ErrNotFound), errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません"
	case errors.Is(err, voicetrain.ErrNoRun):
		return http.StatusConflict, "この登場人物の学習は動いていません"
	}
	status, msg := failure.Fallback(err)
	if status == http.StatusInternalServerError {
		_ = c.Error(err)
	}
	return status, msg
}

func fail(c *gin.Context, err error) {
	status, message := statusOf(c, err)
	settingshttp.Toast(c, status, message)
}

// panelData is what the panel draws of the summary of the training service.
func panelData(domID, url, name string, sum voicetrain.Summary) voicetrainpanel.Data {
	d := voicetrainpanel.Data{
		DOMID: domID, URL: url, Name: name, HasReference: sum.HasReference, DataSeconds: sum.DataSeconds, Problems: sum.Problems,
		Limits: voicetrainpanel.Limits(sum.Limits), Defaults: voicetrainpanel.Epochs{SoVITS: sum.Defaults.EpochsSoVITS, GPT: sum.Defaults.EpochsGPT},
		Max: voicetrainpanel.Epochs{SoVITS: sum.MaxEpochs.EpochsSoVITS, GPT: sum.MaxEpochs.EpochsGPT}, Trained: sum.Trained, GPUBusy: sum.GPUBusy,
	}
	for _, c := range sum.Clips {
		d.Clips = append(d.Clips, voicetrainpanel.Clip{ID: c.ID, Text: c.Text, Duration: c.Duration()})
	}
	if cur := sum.Current; cur != nil {
		d.State, d.Percent, d.Text, d.Failure, d.Error = string(cur.State), cur.Progress.Percent, cur.Text, cur.Failure, cur.Error
	}
	return d
}
