package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/launchargs"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// ErrNotInstalled means the runtime is not installed (or its files are gone).
var ErrNotInstalled = errors.New("runtime: not installed")

// ChoiceEnv is the variable that names the model in force of a runtime with choices, for the launcher (catalog.ResourceSet.Choices).
const ChoiceEnv = "MAM_RUNTIME_CHOICE"

// foreignEnv are the variables of mam's own environment that would point the vendor's interpreter at another Python.
var foreignEnv = []string{"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP", "VIRTUAL_ENV"}

// Command is the launch of the installed runtime on port: the vendor's own recipe (its launcher puts the runtime directory in front
// of PATH and starts `runtime\python.exe -I <script>` in the package directory) with the arguments of the catalog, which bind
// 127.0.0.1 and the port only. The library caches and the temporary files are below the runtime directory, and the catalog's
// environment switches the automatic downloads of the libraries off (offline mode), so that nothing is fetched while it runs and
// nothing is written outside the directory next to mam.exe.
func (i *Installer) Command(port int) (proc.Spec, error) {
	if !i.Status().Installed {
		return proc.Spec{}, ErrNotInstalled
	}
	// The launcher is mam's guard of the server (token, Host, paths): it is the one of this mam at every start, not the one an earlier
	// mam wrote at install time, so a fix of it needs no new download of the package.
	if err := i.writeFiles(i.Layout.EnvDir(), i.Runtime.ExtraFiles); err != nil {
		return proc.Spec{}, err
	}
	l := i.Runtime.Launch
	chosen := i.AssetSet.Chosen(i.Store.Get().Choice) // the model in force ("" for a runtime without models): its file, id and values fill the placeholders
	var model choice.Choice
	for _, c := range i.AssetSet.Choices {
		if c.ID == chosen {
			model = c
		}
	}
	args, err := launchargs.Expand(l.Args, launchargs.Values(port, i.AssetSet.ModelFile(chosen), chosen, model.Values))
	if err != nil {
		return proc.Spec{}, err
	}
	dirs := make([]string, 0, len(l.PathDirs)+1)
	for _, d := range l.PathDirs {
		dirs = append(dirs, filepath.Join(i.Layout.EnvDir(), filepath.FromSlash(d)))
	}
	getenv := i.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if p := getenv("PATH"); p != "" {
		dirs = append(dirs, p)
	}
	cache, tmp := i.Layout.CacheDir(), i.Layout.TempDir()
	for _, d := range []string{cache, tmp} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return proc.Spec{}, err
		}
	}
	env := append([]string{
		"PATH=" + strings.Join(dirs, string(os.PathListSeparator)),
		"HF_HOME=" + filepath.Join(cache, "huggingface"), "TORCH_HOME=" + filepath.Join(cache, "torch"),
		"MODELSCOPE_CACHE=" + filepath.Join(cache, "modelscope"), "XDG_CACHE_HOME=" + cache,
		"TEMP=" + tmp, "TMP=" + tmp, "TMPDIR=" + tmp,
	}, l.Env...)
	if chosen != "" { // its folder is named by MAM_RUNTIME_CHOICE, its settings are its env
		env = append(env, ChoiceEnv+"="+chosen)
		env = append(env, model.Env...)
	}
	return proc.Spec{
		Command: i.ProgramPath(), Args: args, Dir: i.Layout.EnvDir(), LogPath: i.Layout.LogPath(), Env: env, DropEnv: foreignEnv,
	}, nil
}
