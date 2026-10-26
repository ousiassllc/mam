// Package worldcommon holds the output shapes that the world, reference, branch, series and note tools
// share: a Reference Library entry, a Reference Pack entry and a saved version.
package worldcommon

import (
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ReferenceItem is one image of the Reference Library of a Project. asset_id is what get_asset_download_url takes
// to hand out the image itself.
type ReferenceItem struct {
	ID           string    `json:"id" jsonschema:"Reference Library の ID（assign_*_reference と pin_reference に渡す reference_id）"`
	AssetID      string    `json:"asset_id" jsonschema:"画像のアセット ID（get_asset_download_url に渡す）"`
	Kind         string    `json:"kind" jsonschema:"character, product, location, style, lighting"`
	Name         string    `json:"name"`
	Tags         []string  `json:"tags"`
	Source       string    `json:"source" jsonschema:"uploaded（アップロード）か generated（Art Director が生成）"`
	SourceShotID string    `json:"source_shot_id,omitempty" jsonschema:"生成元の Shot（generated のみ）"`
	CreatedAt    time.Time `json:"created_at"`
}

// ReferenceItems converts the Library entries; the result is never nil.
func ReferenceItems(items []domain.ReferenceItem) []ReferenceItem {
	out := make([]ReferenceItem, 0, len(items))
	for _, it := range items {
		tags := it.Tags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, ReferenceItem{
			ID: it.ID, AssetID: it.AssetID, Kind: string(it.Kind), Name: it.Name, Tags: tags,
			Source: string(it.Source), SourceShotID: it.SourceShotID, CreatedAt: it.CreatedAt,
		})
	}
	return out
}

// PackReference is one image of the Reference Pack of a Shot.
type PackReference struct {
	Kind        string `json:"kind"`
	Name        string `json:"name,omitempty"`
	AssetID     string `json:"asset_id"`
	Prompt      string `json:"prompt,omitempty"`
	Model       string `json:"model,omitempty"`
	LibraryItem string `json:"library_item,omitempty" jsonschema:"Library から取った画像のとき、その reference_id"`
}

// PackReferences converts the Reference Pack; the result is never nil.
func PackReferences(refs []domain.Reference) []PackReference {
	out := make([]PackReference, 0, len(refs))
	for _, r := range refs {
		out = append(out, PackReference{
			Kind: string(r.Kind), Name: r.Name, AssetID: r.AssetID, Prompt: r.Prompt, Model: string(r.Model), LibraryItem: r.LibraryItem,
		})
	}
	return out
}

// VersionEntry is one saved version of a Shot spec, a World State, a Branch or a note (newest first in a list).
type VersionEntry struct {
	Number    int       `json:"version_number" jsonschema:"restore_*_version に渡す番号"`
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// VersionEntries converts the versions; the result is never nil.
func VersionEntries(versions []domain.VersionEntry) []VersionEntry {
	out := make([]VersionEntry, 0, len(versions))
	for _, v := range versions {
		out = append(out, VersionEntry{Number: v.Number, Summary: v.Summary, CreatedAt: v.CreatedAt})
	}
	return out
}
