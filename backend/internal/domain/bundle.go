package domain

import (
	"path"
	"strings"
	"time"
)

// Project bundle (spec/requirements/non-functional.md §8): the whole Project as one portable
// archive. The bundle is a ZIP with the manifest (BundleManifestName), the Project Data Model as
// JSON (BundleDataName) and the files the data refers to under BundleAssetsDir, at their
// asset-relative paths.
const (
	BundleFormat       = "mam-project-bundle"
	BundleVersion      = 1
	BundleManifestName = "manifest.json"
	BundleDataName     = "data.json"
	BundleAssetsDir    = "assets/"
)

// BundleManifest identifies a bundle and the Project in it.
type BundleManifest struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ProjectID  string    `json:"project_id"`
	ExportedAt time.Time `json:"exported_at"`
}

// BundleRow is one row of a table: column name to value (string, number or nil).
type BundleRow map[string]any

// BundleTable is the rows of one table of the Project Data Model.
type BundleTable struct {
	Name string      `json:"name"`
	Rows []BundleRow `json:"rows"`
}

// BundleData is the Project Data Model of one Project: every table row that belongs to it
// (data.json). Rows keep their IDs, so a bundle imports into an instance that does not have the
// Project yet and refuses one that does.
type BundleData struct {
	ProjectID string        `json:"project_id"`
	Tables    []BundleTable `json:"tables"`
}

// FilePaths returns the asset-relative paths of the files the data refers to (assets, Takes and
// exports), each once, in a stable order.
func (d BundleData) FilePaths() []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range d.Tables {
		if t.Name != "asset" && t.Name != "take" && t.Name != "export" {
			continue
		}
		for _, row := range t.Rows {
			p, ok := row["file_path"].(string)
			if !ok || p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// ValidBundleFilePath reports whether p is a safe asset-relative path of the Project: slash
// separated, clean, below the Project's own directory, with no ".." and no absolute or drive part.
func ValidBundleFilePath(projectID, p string) bool {
	if !ValidBundleProjectID(projectID) || p == "" || strings.ContainsAny(p, "\\:\x00") {
		return false
	}
	return path.Clean(p) == p && strings.HasPrefix(p, projectID+"/") && len(p) > len(projectID)+1 &&
		!strings.Contains(p, "/../") && !strings.HasSuffix(p, "/..")
}

// ValidBundleProjectID reports whether id can be the name of the Project's file directory.
func ValidBundleProjectID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\:\x00")
}
