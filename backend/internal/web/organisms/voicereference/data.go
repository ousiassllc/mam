// Package voicereference renders the form of the reference voice of a Character (F-20, F-29): the recording a local
// voice provider clones, its transcript and its language, with a player, the upload and the removal. The Series page
// shows it for each canonical Character and the World State page for each Character of the Project.
package voicereference

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// Data is one form. The route URL answers PUT (save), DELETE (remove) and GET (the recording).
type Data struct {
	// DOMID is the id of the form: a save or a removal replaces that form alone.
	DOMID string
	// URL is .../characters/:characterId/voice-reference of the Series or of the Project (with the Branch query).
	URL string
	// Name is the Character, for the accessible names of the controls.
	Name string
	Ref  *voiceref.Reference
}

var languageLabels = map[string]string{"ja": "日本語", "en": "English", "zh": "中文", "ko": "한국어", "yue": "粵語（広東語）"}

// Languages are the options of the language select.
func Languages() []string { return voiceref.Languages() }

// LanguageLabel is the name of a language code.
func LanguageLabel(code string) string {
	if l, ok := languageLabels[code]; ok {
		return l
	}
	return code
}

// Accept is the accept attribute of the file input.
func Accept() string { return strings.Join(voiceref.Extensions(), ",") + ",audio/*" }

// FormatList is the formats for the label: ".wav .mp3 ...".
func FormatList() string { return strings.Join(voiceref.Extensions(), " ") }

// PlayerSrc is the URL of the recording; the name of the stored file makes it new when the recording is replaced,
// so the browser does not play the old one from its cache.
func (d Data) PlayerSrc() string { return d.URL + "?v=" + path.Base(d.Ref.FilePath) }

// Current describes the recording that is set: its length, language and size.
func (d Data) Current() string {
	r := d.Ref
	length := "長さ未確認"
	if r.DurationMS > 0 {
		length = fmt.Sprintf("%.1f 秒", r.Duration().Round(100*time.Millisecond).Seconds())
	}
	return length + " · " + LanguageLabel(r.Language) + " · " + strconv.FormatInt((r.SizeBytes+1023)/1024, 10) + " KB"
}

// Unmeasured reports that the length of the recording was not checked (ffprobe was not available).
func (d Data) Unmeasured() bool { return d.Ref != nil && d.Ref.DurationMS == 0 }
