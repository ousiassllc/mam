package bundlezip

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// The reference voice of a Character (domain/voiceref) is a JSON object in the voice_reference column of the
// character rows, so its file is named inside the row, not in an asset row: the paths of the data are
// found here, and a file that lives in another directory is moved into the exported Project like a shared image.

// dataPaths is every asset-relative path the data refers to: the files of the asset, take and export rows, of the
// Audio Timelines and of the reference voices, each once, in a stable order.
func dataPaths(data domain.BundleData) ([]string, error) {
	timeline, err := data.AudioTimelinePaths()
	if err != nil {
		return nil, fmt.Errorf("audio timeline: %w", err)
	}
	voices, err := voiceReferencePaths(data)
	if err != nil {
		return nil, err
	}
	return append(append(data.FilePaths(), timeline...), voices...), nil
}

// voiceRows calls fn for every character row that has a reference voice.
func voiceRows(data domain.BundleData, fn func(row domain.BundleRow, ref voiceref.Reference) error) error {
	for _, t := range data.Tables {
		if t.Name != "character" {
			continue
		}
		for _, row := range t.Rows {
			raw, _ := row["voice_reference"].(string)
			if raw == "" {
				continue
			}
			var ref voiceref.Reference
			if err := json.Unmarshal([]byte(raw), &ref); err != nil {
				return fmt.Errorf("character voice reference: %w", err)
			}
			if err := fn(row, ref); err != nil {
				return err
			}
		}
	}
	return nil
}

func voiceReferencePaths(data domain.BundleData) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	err := voiceRows(data, func(_ domain.BundleRow, ref voiceref.Reference) error {
		if ref.FilePath != "" && !seen[ref.FilePath] {
			seen[ref.FilePath] = true
			out = append(out, ref.FilePath)
		}
		return nil
	})
	return out, err
}

// relocateSharedVoices gives the reference voices whose file lives in another directory (the Series' directory,
// or the Project an Episode copied the canon from) a path of the exported Project: <project>/<character id>-voice<ext>.
// One file that several rows share is moved once. It adds the new path to the original one in sources.
func relocateSharedVoices(data *domain.BundleData, sources map[string]string) error {
	moved := map[string]string{} // original path -> new path
	return voiceRows(*data, func(row domain.BundleRow, ref voiceref.Reference) error {
		owner, _, _ := strings.Cut(ref.FilePath, "/")
		id, _ := row["id"].(string)
		if ref.FilePath == "" || owner == data.ProjectID || !bundlepath.ValidFilePath(owner, ref.FilePath) || !bundlepath.ValidID(id) {
			return nil
		}
		to, ok := moved[ref.FilePath]
		if !ok {
			to = data.ProjectID + "/" + id + "-voice" + path.Ext(ref.FilePath)
			moved[ref.FilePath] = to
			sources[to] = ref.FilePath
		}
		ref.FilePath = to
		raw, err := json.Marshal(ref)
		if err != nil {
			return err
		}
		row["voice_reference"] = string(raw)
		return nil
	})
}
