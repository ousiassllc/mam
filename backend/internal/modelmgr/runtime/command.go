package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// ErrNotInstalled means the runtime is not installed (or its files are gone).
var ErrNotInstalled = errors.New("runtime: not installed")

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
	l := i.Runtime.Launch
	args := make([]string, len(l.Args))
	for n, a := range l.Args {
		args[n] = strings.ReplaceAll(a, "{port}", fmt.Sprint(port))
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
	return proc.Spec{
		Command: i.PythonPath(), Args: args, Dir: i.Layout.EnvDir(), LogPath: i.Layout.LogPath(), Env: env, DropEnv: foreignEnv,
	}, nil
}
