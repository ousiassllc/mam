package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

// An archive runtime has the launcher of mam's next to the vendor's server after every install, and a newer install writes it again.
func TestInstallWritesTheExtraFilesOfAnArchiveRuntimeEveryTime(t *testing.T) {
	r := newRig(t, "1")
	r.Runtime.ExtraFiles = []parts.File{{Dest: "ComfyUI/mam_launcher.py", Overlay: "mam_launcher.py"}}
	r.Overlays = map[string][]byte{"mam_launcher.py": []byte("# v1\n")}
	target := filepath.Join(r.Layout.EnvDir(), "ComfyUI", "mam_launcher.py")

	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "# v1\n", string(got))
	assert.True(t, r.Status().Installed)

	r.Runtime.Version = "2" // the update: the environment is extracted again, with the launcher of this mam
	r.Overlays = map[string][]byte{"mam_launcher.py": []byte("# v2\n")}
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	got, err = os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "# v2\n", string(got))
}

// The launcher is the one of this mam at every start: an installed one that an older mam wrote is replaced before the server runs.
func TestCommandWritesTheCurrentExtraFilesBeforeEveryStart(t *testing.T) {
	r := newRig(t, "1")
	r.Runtime.ExtraFiles = []parts.File{{Dest: "mam_launcher.py", Overlay: "mam_launcher.py"}}
	r.Overlays = map[string][]byte{"mam_launcher.py": []byte("# old\n")}
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	target := filepath.Join(r.Layout.EnvDir(), "mam_launcher.py")
	r.Overlays = map[string][]byte{"mam_launcher.py": []byte("# fixed\n")} // a newer mam, the same catalog version

	_, err := r.Command(9880)

	require.NoError(t, err)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "# fixed\n", string(got))
}

// An environment installed before the launcher existed is Broken (install again), not started with a launcher that is not there.
func TestStatusIsBrokenWhenTheExtraFilesAreNotThere(t *testing.T) {
	r := newRig(t, "1")
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	require.True(t, r.Status().Installed)

	r.Runtime.ExtraFiles = []parts.File{{Dest: "mam_launcher.py", Overlay: "mam_launcher.py"}}

	st := r.Status()
	assert.False(t, st.Installed)
	assert.True(t, st.Broken)
}

func TestInstallRefusesAnExtraFileWhoseOverlayIsNotThereAndKeepsTheInstalledEnvironment(t *testing.T) {
	r := newRig(t, "1")
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	r.Runtime.ExtraFiles = []parts.File{{Dest: "mam_launcher.py", Overlay: "nope.py"}}

	err := r.Install(context.Background(), catalog.VariantNVIDIA, nil)

	require.ErrorContains(t, err, `no overlay "nope.py"`)
	assert.NoDirExists(t, r.Layout.EnvNewDir())
	assert.FileExists(t, r.PythonPath())
}
