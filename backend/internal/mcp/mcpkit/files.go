package mcpkit

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// FilesPath is where the MCP listener serves the files: FilesPath + ticket.
const FilesPath = "/mcp-files/"

// TicketTTL is how long a ticket works.
const TicketTTL = 15 * time.Minute

const (
	ticketBytes = 24
	maxTickets  = 10000
)

// DownloadFile is a file a download kind hands out. Set Path (an absolute path of a file; range
// requests work) or Write (the bytes are made while they are sent, e.g. a zip).
type DownloadFile struct {
	Name        string // the file name offered to the client
	ContentType string // "" lets the server decide from Name
	Path        string
	Write       func(w io.Writer) error
}

// DownloadFunc finds the file of the resource id of a kind. It runs when the URL is made (a missing
// resource is an error of the tool, not a 404 later) and again at each download, so it must be cheap and
// must not change anything: put the work into Write. Return service errors as they are.
type DownloadFunc func(ctx context.Context, id string) (DownloadFile, error)

// UploadParam describes one value a ticket of a purpose is bound to.
type UploadParam struct {
	Name        string
	Description string
	Required    bool
}

// UploadInput is what the file endpoint hands to the handler of a purpose.
type UploadInput struct {
	// Params are the values the ticket was bound to, validated against the purpose's Params.
	Params map[string]string
	// Header is the uploaded file (form field "file"). Its content is in memory or a temporary file.
	Header *multipart.FileHeader
}

// Upload is the file as the service layer takes it (service/upload.go validates it).
func (in UploadInput) Upload() service.Upload {
	return service.Upload{
		Name: in.Header.Filename,
		Size: in.Header.Size,
		Open: func() (io.ReadCloser, error) { return in.Header.Open() },
	}
}

// UploadSpec is one purpose a file can be uploaded for ("import_bundle", "reference_image", ...).
type UploadSpec struct {
	Purpose     string
	Description string
	// MaxBytes bounds the file; the request is refused above it.
	MaxBytes int64
	Params   []UploadParam
	// Handle stores the file through the service layer and returns what the upload answers with (any value
	// that encodes to JSON: ids of what was made). Return service errors as they are.
	Handle func(ctx context.Context, in UploadInput) (any, error)
}

// Files issues the tickets for the file endpoint and serves it (see Handler). A ticket is a random value
// bound to one direction, one kind and one resource, valid for TicketTTL, kept in memory only. A download
// ticket works until it expires (a client may retry or ask for a range); an upload ticket works once.
type Files struct {
	now func() time.Time

	mu        sync.Mutex
	downloads map[string]DownloadFunc
	uploads   map[string]UploadSpec
	tickets   map[string]*ticket

	// authorize tells whether the token that issued a ticket still exists: a revoked token's tickets stop at once.
	authorize func(ctx context.Context, tokenID string) bool
	// fallbackBase is the base URL when the request that asked for a ticket did not say which host it used.
	fallbackBase func() string
	logger       *slog.Logger
}

// FilesOptions are the collaborators of Files. Every field is optional.
type FilesOptions struct {
	// Authorize tells whether the token that issued a ticket still exists; nil counts every issuer as active.
	Authorize func(ctx context.Context, tokenID string) bool
	// FallbackBase is the base URL for a request that did not say which host it used; nil makes such a URL relative.
	FallbackBase func() string
	// Logger receives the causes of internal upload errors.
	Logger *slog.Logger
}

// NewFiles returns Files.
func NewFiles(opts FilesOptions) *Files {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Files{
		now:          time.Now,
		downloads:    map[string]DownloadFunc{},
		uploads:      map[string]UploadSpec{},
		tickets:      map[string]*ticket{},
		authorize:    opts.Authorize,
		fallbackBase: opts.FallbackBase,
		logger:       opts.Logger,
	}
}

// RegisterDownload installs a kind of downloadable file ("take_video", "project_asset", ...).
func (f *Files) RegisterDownload(kind string, fn DownloadFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.downloads[kind]; dup {
		panic(fmt.Sprintf("mcpkit: download kind %q is registered twice", kind))
	}
	f.downloads[kind] = fn
}

// RegisterUpload installs a purpose files can be uploaded for.
func (f *Files) RegisterUpload(spec UploadSpec) {
	if spec.Purpose == "" || spec.Handle == nil || spec.MaxBytes <= 0 {
		panic(fmt.Sprintf("mcpkit: upload purpose %q needs a name, a handler and a size limit", spec.Purpose))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.uploads[spec.Purpose]; dup {
		panic(fmt.Sprintf("mcpkit: upload purpose %q is registered twice", spec.Purpose))
	}
	f.uploads[spec.Purpose] = spec
}

// UploadPurposes lists the registered purposes, in name order.
func (f *Files) UploadPurposes() []UploadSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]UploadSpec, 0, len(f.uploads))
	for _, s := range f.uploads {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b UploadSpec) int { return strings.Compare(a.Purpose, b.Purpose) })
	return out
}

// Link is a ticket URL, for a tool to put into its output.
type Link struct {
	URL       string    `json:"url" jsonschema:"15 分だけ有効なトークン付き URL / URL with a ticket, valid for 15 minutes"`
	ExpiresAt time.Time `json:"expires_at"`
	FileName  string    `json:"file_name,omitempty"`
}

// DownloadURL returns a URL that downloads the file of the resource id of a kind (GET, no other
// credential needed). It is service.ErrNotFound when the resource has no file, and the error of the
// kind's DownloadFunc otherwise. Tools call it to put "video_url", "export_url", ... into their output.
func (f *Files) DownloadURL(ctx context.Context, kind, id string) (Link, error) {
	f.mu.Lock()
	fn := f.downloads[kind]
	f.mu.Unlock()
	if fn == nil {
		return Link{}, fmt.Errorf("mcpkit: download kind %q is not registered", kind)
	}
	file, err := fn(ctx, id)
	if err != nil {
		return Link{}, err
	}
	t, err := f.issue(ctx, &ticket{dir: download, kind: kind, id: id})
	if err != nil {
		return Link{}, err
	}
	return Link{URL: f.url(ctx, t.value), ExpiresAt: t.expires, FileName: file.Name}, nil
}

// UploadTicket is what create_upload_ticket answers with.
type UploadTicket struct {
	Link
	Purpose     string            `json:"purpose"`
	Method      string            `json:"method" jsonschema:"HTTP メソッド（POST）"`
	ContentType string            `json:"content_type" jsonschema:"multipart/form-data"`
	FileField   string            `json:"file_field" jsonschema:"ファイルを入れるフォームのフィールド名 / form field of the file"`
	Headers     map[string]string `json:"headers" jsonschema:"必須の追加ヘッダー（なし）/ extra headers the request needs (none: the URL carries the ticket)"`
	MaxBytes    int64             `json:"max_bytes"`
	Curl        string            `json:"curl_example"`
}

// UploadTicketFor issues a one-time upload ticket for a purpose, bound to params. A purpose that is not
// registered, a parameter it does not declare and a missing required parameter are ValidationErrors.
func (f *Files) UploadTicketFor(ctx context.Context, purpose string, params map[string]string) (UploadTicket, error) {
	f.mu.Lock()
	spec, ok := f.uploads[purpose]
	f.mu.Unlock()
	if !ok {
		return UploadTicket{}, &service.ValidationError{Field: "purpose", Message: "未対応の用途です: " + purpose}
	}
	if err := checkParams(spec, params); err != nil {
		return UploadTicket{}, err
	}
	t, err := f.issue(ctx, &ticket{dir: upload, kind: purpose, params: params})
	if err != nil {
		return UploadTicket{}, err
	}
	link := f.url(ctx, t.value)
	return UploadTicket{
		Link: Link{URL: link, ExpiresAt: t.expires}, Purpose: purpose, Method: "POST",
		ContentType: "multipart/form-data", FileField: FileField, Headers: map[string]string{},
		MaxBytes: spec.MaxBytes, Curl: fmt.Sprintf("curl -X POST -F '%s=@/path/to/file' '%s'", FileField, link),
	}, nil
}

// FileField is the form field the uploaded file is in.
const FileField = "file"

func checkParams(spec UploadSpec, params map[string]string) error {
	declared := map[string]bool{}
	for _, p := range spec.Params {
		declared[p.Name] = true
		if p.Required && strings.TrimSpace(params[p.Name]) == "" {
			return &service.ValidationError{Field: p.Name, Message: p.Name + " は必須です"}
		}
	}
	for name := range params {
		if !declared[name] {
			return &service.ValidationError{Field: name, Message: "この用途では使えない項目です"}
		}
	}
	return nil
}
