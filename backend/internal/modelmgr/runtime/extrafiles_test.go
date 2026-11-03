package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"

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
	assert.FileExists(t, r.ProgramPath())
}

// A tool script runs with the interpreter of the runtime, with all the tool files of this mam written first (one may import another); a name that is
// not a tool file is refused, and nothing is written for it.
func TestToolWritesTheToolFilesAndRefusesWhatIsNotOne(t *testing.T) {
	r := newRig(t, "1")
	r.Runtime.ToolFiles = []parts.File{{Dest: "driver.py", Overlay: "driver.py"}, {Dest: "driver_job.py", Overlay: "driver_job.py"}}
	r.Overlays = map[string][]byte{"driver.py": []byte("# driver\n"), "driver_job.py": []byte("# job\n")}

	_, err := r.Tool("driver.py", []string{"text"}, nil)
	require.ErrorIs(t, err, rtm.ErrNotInstalled, "an environment that is not installed has nothing to run it in")

	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	spec, err := r.Tool("driver.py", []string{"text", "/work/job.json"}, []string{"A=1"})

	require.NoError(t, err)
	assert.Equal(t, []string{"-I", "-u", "-X", "utf8", "driver.py", "text", "/work/job.json"}, spec.Args)
	assert.Contains(t, spec.Env, "A=1")
	for name, want := range map[string]string{"driver.py": "# driver\n", "driver_job.py": "# job\n"} {
		got, err := os.ReadFile(filepath.Join(r.Layout.EnvDir(), name))
		require.NoError(t, err)
		assert.Equal(t, want, string(got))
	}

	for _, script := range []string{"../evil.py", "ComfyUI/main.py", "driver.py ", ""} {
		_, err := r.Tool(script, nil, nil)
		assert.ErrorIs(t, err, rtm.ErrNotATool, script)
	}
}
