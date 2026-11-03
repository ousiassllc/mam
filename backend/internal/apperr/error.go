package apperr

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/ousiassllc/mam/backend/internal/logging"
)

// Error is a failure in the shape a person reads. It is an error: the text of Error() is for a log (the code and the detail), the
// words for a person come from View.
type Error struct {
	Code Code
	// Args fill the {name} of the texts of the code in the catalog ({param} of "input_missing", {after} of "stalled").
	Args map[string]string
	// Cause, when set, is the cause in words, already in the language the producer chose: a service that worded its refusal keeps its
	// sentence, and the catalog still adds the remedy. "" uses the text of the catalog.
	Cause string
	// Detail is what the machine said: the original message, a node number, an id. It is never the first thing a screen shows
	// (the component folds it) and it may be in English.
	Detail string
	// ID is the correlation id of a failure that was logged (NewID); "" for a failure that was not. The log line has it as error_id.
	ID string
	// Err is the failure behind it, for errors.Is and errors.As.
	Err error
}

// New is an Error of code. Add args with With, the detail with WithDetail and the failure behind with Wrap.
func New(code Code) *Error { return &Error{Code: code} }

// With sets an argument of the texts and returns e. An empty value is kept: the text then reads without it.
func (e *Error) With(name, value string) *Error {
	if e.Args == nil {
		e.Args = map[string]string{}
	}
	e.Args[name] = value
	return e
}

// WithDetail sets the detail (a secret in it is hidden, logging.Redact) and returns e.
func (e *Error) WithDetail(detail string) *Error {
	e.Detail = logging.Redact(detail)
	return e
}

// WithCause sets the fixed cause (see Error.Cause) and returns e.
func (e *Error) WithCause(cause string) *Error {
	e.Cause = cause
	return e
}

// Wrap sets the failure behind e and returns it; the detail is the text of it unless a detail is set.
func (e *Error) Wrap(err error) *Error {
	e.Err = err
	if e.Detail == "" && err != nil {
		e.Detail = logging.Redact(err.Error())
	}
	return e
}

// Error is the log text: "<code>: <detail>", or the code alone.
func (e *Error) Error() string {
	if e.Detail == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Detail
}

// Unwrap is the failure behind e.
func (e *Error) Unwrap() error { return e.Err }

// Class is the coarse class of the code (ClassValidation, ...). A code that is not in the catalog is ClassInternal.
func (e *Error) Class() string { return specs[e.Code].class }

// statusOfClass is the HTTP status of a class: what the screens and the file endpoint of the MCP answer with for the failure.
var statusOfClass = map[string]int{
	ClassValidation: http.StatusUnprocessableEntity, ClassNotFound: http.StatusNotFound, ClassConflict: http.StatusConflict,
	ClassLocked: http.StatusLocked, ClassForbidden: http.StatusForbidden, ClassBudget: http.StatusPaymentRequired,
	ClassRateLimited: http.StatusTooManyRequests, ClassUnavailable: http.StatusServiceUnavailable,
	ClassUpstream: http.StatusBadGateway, ClassInternal: http.StatusInternalServerError,
}

// Status is the HTTP status of the failure, by its class (spec/api/errors.md): 422 for an input, 502 for an outside service, 503 for a feature that is not set up, 500 for an
// unknown failure.
func (e *Error) Status() int {
	if st, ok := statusOfClass[e.Class()]; ok {
		return st
	}
	return http.StatusInternalServerError
}

// As finds the *Error in the chain of err.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// NewID returns the correlation id of one logged failure.
func NewID() string { return "err_" + uuid.Must(uuid.NewV7()).String() }

// Link is a screen that helps: the words of the link and its path.
type Link struct {
	Label string `json:"label" jsonschema:"リンクの文言 / words of the link"`
	Path  string `json:"path" jsonschema:"mam の画面のパス（/settings/api-keys など）/ path of the screen in mam"`
}

// View is a failure in words, what a screen draws and a tool returns.
type View struct {
	Code     Code   `json:"code" jsonschema:"失敗の種類のコード（vram_oom・input_missing・llm_key_missing など。apperr のコード一覧）/ kind of the failure"`
	Class    string `json:"class" jsonschema:"失敗の大分類（validation・upstream・unavailable・internal など。エラーのコードと同じ値）/ coarse class of the failure"`
	Cause    string `json:"cause" jsonschema:"日本語で、なぜ失敗したか / why it failed, in words"`
	Remedy   string `json:"remedy,omitempty" jsonschema:"日本語で、どうすればよいか / what to do"`
	Settings []Link `json:"settings,omitempty" jsonschema:"直すのに開く画面（設定画面など）/ screens that help"`
	Detail   string `json:"detail,omitempty" jsonschema:"機械が返した詳細（元のメッセージ・ノード番号・ID など。英語のことがある）/ what the machine said"`
	ErrorID  string `json:"error_id,omitempty" jsonschema:"ログに残した失敗の相関 ID。get_app_logs の attrs に {\"error_id\": …} で渡すと原因のログ行が見つかる / correlation id of the logged failure"`
}

// View puts e in words in lang (a language without text falls back to Japanese).
func (e *Error) View(lang Lang) View {
	s := specs[e.Code]
	v := View{Code: e.Code, Class: s.class, Cause: e.Cause, Remedy: Text(lang, string(e.Code)+".remedy", e.Args), Detail: e.Detail, ErrorID: e.ID}
	if v.Cause == "" {
		v.Cause = Text(lang, string(e.Code)+".cause", e.Args)
	}
	for _, p := range s.places {
		v.Settings = append(v.Settings, Link{Label: Text(lang, "place."+string(p), nil), Path: places[p]})
	}
	return v
}

// Sentence is the cause and the remedy as one text, for a place that has room for a line only (a chat message, a toast, the text of a
// tool error): the cause first.
func (v View) Sentence(lang Lang) string {
	return strings.TrimSpace(v.Cause + separator(lang) + v.Remedy)
}

// Record is e as it is kept: in the history of a result, so that the failure can be put in words again, in any language, later.
// The words are not kept (a better text in a later version reaches the old failures too); the fixed Cause is, as it has no other source.
type Record struct {
	Code   Code              `json:"code"`
	Args   map[string]string `json:"args,omitempty"`
	Cause  string            `json:"cause,omitempty"`
	Detail string            `json:"detail,omitempty"`
	ID     string            `json:"id,omitempty"`
}

// Marshal is e as the text a column keeps (a JSON object).
func (e *Error) Marshal() string {
	b, err := json.Marshal(Record{Code: e.Code, Args: e.Args, Cause: e.Cause, Detail: e.Detail, ID: e.ID})
	if err != nil { // cannot happen: strings only
		return ""
	}
	return string(b)
}

// Unmarshal reads what Marshal made. ok is false for "" (no failure kept) and for text that is not a record or whose code is unknown.
func Unmarshal(s string) (e *Error, ok bool) {
	if s == "" {
		return nil, false
	}
	var r Record
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, false
	}
	if _, known := specs[r.Code]; !known {
		return nil, false
	}
	return &Error{Code: r.Code, Args: r.Args, Cause: r.Cause, Detail: r.Detail, ID: r.ID}, true
}

// Report is the text the copy button of the error box puts on the clipboard, for a bug report: the code, the cause, the remedy,
// the detail and the error ID, one per line.
func (v View) Report(lang Lang) string {
	var b strings.Builder
	b.WriteString(string(v.Code))
	for _, s := range []string{v.Cause, v.Remedy} {
		if s != "" {
			b.WriteString("\n" + s)
		}
	}
	if v.ErrorID != "" {
		b.WriteString("\n" + Text(lang, "ui.error_id", nil) + ": " + v.ErrorID)
	}
	if v.Detail != "" {
		b.WriteString("\n\n" + v.Detail)
	}
	return b.String()
}

// maxLineDetailRunes bounds the detail in Line.
const maxLineDetailRunes = 200

// Line is the failure as one line of text, for a place that keeps or shows a sentence only (the history column "error", a chat message, the
// text of a tool error): the cause and the remedy, then the start of the detail in brackets.
func (v View) Line(lang Lang) string {
	line := v.Sentence(lang)
	if v.Detail == "" {
		return line
	}
	d := []rune(strings.Join(strings.Fields(v.Detail), " "))
	if len(d) > maxLineDetailRunes {
		d = append(d[:maxLineDetailRunes], '…')
	}
	if lang == LangEn {
		return line + " (detail: " + string(d) + ")"
	}
	return line + "（詳細: " + string(d) + "）"
}

// reasoned is the error of a job that failed: its text is the sentence the user was told, and the *Error behind it is what errors.As finds,
// so that the job queue keeps the code with the sentence.
type reasoned struct {
	text string
	err  *Error
}

func (r *reasoned) Error() string { return r.text }
func (r *reasoned) Unwrap() error { return r.err }

// WithText is an error whose text is text and whose failure is e (errors.As finds e): the error a job returns, with the sentence it told the user.
func WithText(text string, e *Error) error { return &reasoned{text: text, err: e} }
