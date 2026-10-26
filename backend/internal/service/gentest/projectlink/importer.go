package projectlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// How a result is imported (ImportRequest.As).
const (
	AsReference = "reference" // an asset of type reference, registered in the Project's Reference Library
	AsAsset     = "asset"     // an asset of type image or video
)

// AssignTarget is the optional second step of an import as a reference: the Character, Product or Location and the slot
// ("face", "body", "geometry", "label", "layout") the new Library image is put on. The slot's kind must be the reference's kind.
type AssignTarget struct {
	Target string `json:"target" jsonschema:"character / product / location"`
	ID     string `json:"id" jsonschema:"Character・Product・Location の ID"`
	Slot   string `json:"slot" jsonschema:"face / body（Character）、geometry / label（Product）、layout（Location）"`
}

// ImportRequest imports a succeeded result into a Project. Any succeeded result can be imported: the favorite mark is only a hint.
type ImportRequest struct {
	ResultID  string `json:"result_id"`
	ProjectID string `json:"project_id"`
	As        string `json:"as" jsonschema:"reference（Reference Library の参照。画像のみ）/ asset（アセット）"`
	// Kind, Name and Tags are for As reference: the library kind (character / product / location / style / lighting), the name (up to
	// 100 characters) and the tags (comma separated, up to 10 of 30 characters).
	Kind   string        `json:"kind,omitempty"`
	Name   string        `json:"name,omitempty"`
	Tags   string        `json:"tags,omitempty"`
	Assign *AssignTarget `json:"assign,omitempty"`
}

// ImportTarget is how a result was imported: as a reference or an asset, with the library entry's kind, name and tags, and the slot it was assigned to.
type ImportTarget struct {
	As     string        `json:"as"`
	Kind   string        `json:"kind,omitempty"`
	Name   string        `json:"name,omitempty"`
	Tags   []string      `json:"tags,omitempty"`
	Assign *AssignTarget `json:"assign,omitempty"`
}

// ImportOutcome is what an import made.
type ImportOutcome struct {
	ImportID        string `json:"import_id"`
	ProjectID       string `json:"project_id"`
	AssetID         string `json:"asset_id"`
	ReferenceItemID string `json:"reference_id,omitempty"`
	Assigned        bool   `json:"assigned"`
}

// PartialImportError says the file was imported but the second step (the assignment to a slot) was refused: the Library image
// exists, the slot is as it was. Err is the refusal (a locked slot, a Character that is not there, ...).
type PartialImportError struct {
	Outcome ImportOutcome
	Err     error
}

func (e *PartialImportError) Error() string {
	return fmt.Sprintf("the image was imported (reference %s) but not assigned to the slot: %v", e.Outcome.ReferenceItemID, e.Err)
}

// Unwrap returns the refusal of the assignment.
func (e *PartialImportError) Unwrap() error { return e.Err }

// Import copies the file of a succeeded result into the Project's assets directory and registers it (as a reference of
// the Reference Library with source "generated", or as an asset), records where it went (gentest_import) and, when asked, assigns
// the new Library image to a slot of a Character, Product or Location. Nothing of the history changes.
func (l *Link) Import(ctx context.Context, req ImportRequest) (ImportOutcome, error) {
	res, err := l.Repo.GetResult(ctx, req.ResultID)
	if err != nil {
		return ImportOutcome{}, gterr.NotFound(err)
	}
	if res.Status != gentestrepo.StatusSucceeded || res.FilePath == "" {
		return ImportOutcome{}, gterr.Conflict("取り込めるのは成功した結果だけです（この結果は %s）", res.Status)
	}
	if _, err := l.Projects.Get(ctx, req.ProjectID); errors.Is(err, repository.ErrNotFound) {
		return ImportOutcome{}, gterr.ErrNotFound
	} else if err != nil {
		return ImportOutcome{}, err
	}
	kind, name, tags, err := l.checkImport(req, res)
	if err != nil {
		return ImportOutcome{}, err
	}
	out := ImportOutcome{ProjectID: req.ProjectID, AssetID: domain.NewID()}
	asset, err := l.copyIn(res, req.ProjectID, out.AssetID)
	if err != nil {
		return ImportOutcome{}, err
	}
	switch req.As {
	case AsReference:
		asset.Type = domain.AssetReference
		out.ReferenceItemID = domain.NewID()
		item := domain.ReferenceItem{ID: out.ReferenceItemID, ProjectID: req.ProjectID, AssetID: asset.ID, Kind: kind, Name: name, Tags: tags, Source: domain.SourceGenerated}
		err = l.References.Add(ctx, item, asset)
	default:
		asset.Type = domain.AssetImage
		if res.Media == gentestrepo.MediaVideo {
			asset.Type = domain.AssetVideo
		}
		err = l.Repo.AddProjectAsset(ctx, asset)
	}
	if err != nil {
		_ = os.Remove(l.assetPath(asset.FilePath))
		return ImportOutcome{}, gterr.NotFound(err)
	}
	target, _ := json.Marshal(ImportTarget{As: req.As, Kind: string(kind), Name: name, Tags: tags, Assign: req.Assign})
	rec, err := l.Repo.AddImport(context.WithoutCancel(ctx), gentestrepo.Import{
		ResultID: res.ID, ProjectID: req.ProjectID, AssetID: asset.ID, ReferenceItemID: out.ReferenceItemID, Target: target, CreatedAt: l.Now().UTC(),
	})
	if err != nil {
		// The file is in the Project; the record of where it went is the history's, not a condition of the import.
		l.Logger.Error("record the import of a generation test result failed", "result_id", res.ID, "project_id", req.ProjectID, "err", err)
	}
	out.ImportID = rec.ID
	if req.Assign != nil {
		if err := l.Assign(ctx, req.ProjectID, domain.WorldTarget(req.Assign.Target), req.Assign.ID, req.Assign.Slot, out.ReferenceItemID); err != nil {
			return out, &PartialImportError{Outcome: out, Err: err}
		}
		out.Assigned = true
	}
	return out, nil
}

// checkImport checks what the import asks for before anything is written.
func (l *Link) checkImport(req ImportRequest, res gentestrepo.Result) (kind domain.ReferenceKind, name string, tags []string, err error) {
	switch req.As {
	case AsAsset:
		if req.Assign != nil {
			return "", "", nil, gterr.Invalid("スロットへの割り当ては、参照（as=reference）として取り込むときだけ使えます")
		}
		return "", "", nil, nil
	case AsReference:
	default:
		return "", "", nil, gterr.Invalid("取り込み方を選んでください（reference / asset）")
	}
	if res.Media != gentestrepo.MediaImage {
		return "", "", nil, gterr.Invalid("動画は参照にできません。アセットとして取り込んでください")
	}
	kind = domain.ReferenceKind(strings.TrimSpace(req.Kind))
	if !domain.ValidLibraryKind(kind) {
		return "", "", nil, gterr.Invalid("種別を選んでください（character / product / location / style / lighting）")
	}
	name = strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > domain.MaxReferenceNameRunes {
		return "", "", nil, gterr.Invalid("名前は 1〜%d 文字で入力してください", domain.MaxReferenceNameRunes)
	}
	if tags, err = domain.ParseTags(req.Tags); err != nil {
		return "", "", nil, gterr.Invalid("タグは各 %d 文字以内・%d 個までにしてください", domain.MaxReferenceTagRunes, domain.MaxReferenceTags)
	}
	if a := req.Assign; a != nil {
		if l.Assign == nil {
			return "", "", nil, gterr.ErrUnavailable
		}
		slot, ok := domain.FindReferenceSlot(domain.WorldTarget(a.Target), a.Slot)
		switch {
		case !ok:
			return "", "", nil, gterr.Invalid("この対象には指定の参照枠がありません（%s の %s）", a.Target, a.Slot)
		case a.ID == "":
			return "", "", nil, gterr.Invalid("割り当て先の ID を指定してください")
		case slot.Kind != kind:
			return "", "", nil, gterr.Invalid("%s の %s には %s の参照画像を選んでください", a.Target, a.Slot, slot.Kind.Label())
		}
	}
	return kind, name, tags, nil
}

// copyIn copies the result's file to <assets>/<projectID>/<assetID><ext> and returns the asset row for it.
func (l *Link) copyIn(res gentestrepo.Result, projectID, assetID string) (domain.Asset, error) {
	if l.AssetsDir == "" {
		return domain.Asset{}, errors.New("imports are not configured: assets directory is empty")
	}
	rel := path.Join(projectID, assetID+filepath.Ext(res.FilePath))
	dst := l.assetPath(rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return domain.Asset{}, fmt.Errorf("create asset directory: %w", err)
	}
	if err := fsutil.CopyFile(l.filePath(res.FilePath), dst); err != nil {
		return domain.Asset{}, fmt.Errorf("copy the result into the project: %w", err)
	}
	return domain.Asset{ID: assetID, ProjectID: projectID, FilePath: rel, MimeType: res.MimeType, SizeBytes: res.SizeBytes}, nil
}

func (l *Link) assetPath(rel string) string {
	return filepath.Join(l.AssetsDir, filepath.FromSlash(rel))
}
