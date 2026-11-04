package runtime

import (
	"errors"
	"fmt"
	"os"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// RemoveAssets deletes the files of the asset set (and their unfinished downloads) and the record of them, and nothing else: the directory
// the files are in is the user's own when the layout's Env is set (Remove would clear it whole). A file that is not there is not an error.
func (i *Installer) RemoveAssets() error {
	for _, a := range i.AssetSet.Assets {
		for _, f := range a.Files() {
			target := i.envPath(f.Dest)
			for _, p := range []string{target, fetch.PartPath(target)} {
				if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("runtime: remove %s: %w", f.Dest, err)
				}
			}
		}
	}
	return i.forgetAssets()
}
