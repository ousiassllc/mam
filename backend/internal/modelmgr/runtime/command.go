package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// ErrNotInstalled means the runtime is not installed (or its files are gone).
var ErrNotInstalled = errors.New("runtime: not installed")

// ErrNotATool means the script is not one of the tool files of the runtime: nothing but those is ever started by Tool.
var ErrNotATool = errors.New("runtime: not a tool file of the runtime")

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
	args := make([]string, len(l.Args))
	for n, a := range l.Args {
		args[n] = strings.ReplaceAll(a, "{port}", fmt.Sprint(port))
	}
	spec, err := i.spec(args, nil)
	if err != nil {
		return proc.Spec{}, err
	}
	if id := i.AssetSet.Chosen(i.Store.Get().Choice); id != "" { // the model in force: its folder is named by MAM_RUNTIME_CHOICE, its settings are its env
		spec.Env = append(spec.Env, ChoiceEnv+"="+id)
		for _, c := range i.AssetSet.Choices {
			if c.ID == id {
				spec.Env = append(spec.Env, c.Env...)
			}
		}
	}
	return spec, nil
}

// Tool is the launch of a script of mam's own (Runtime.ToolFiles: the fine-tuning driver) with the interpreter and the environment of the
// installed runtime, in the same place and with the same rules as the server (the catalog's offline environment, the caches and the
// temporary files below the runtime directory, no PYTHONPATH of the user): a job other than the server that runs in the package. script is
// the dest of one of the tool files and nothing else (ErrNotATool); args follow it; env is added. The tool files are written again before the
// script runs, so they are the ones of this mam. The caller gives the spec a LogPath, and runs it (proc.StartPiped).
func (i *Installer) Tool(script string, args, env []string) (proc.Spec, error) {
	if !i.Status().Installed {
		return proc.Spec{}, ErrNotInstalled
	}
	if !slices.ContainsFunc(i.Runtime.ToolFiles, func(f parts.File) bool { return f.Dest == script }) {
		return proc.Spec{}, fmt.Errorf("%w: %q", ErrNotATool, script)
	}
	if err := i.writeFiles(i.Layout.EnvDir(), i.Runtime.ToolFiles); err != nil { // all of them: a tool may import another of mam's own
		return proc.Spec{}, err
	}
	spec, err := i.spec(append([]string{"-I", "-u", "-X", "utf8", script}, args...), env)
	if err != nil {
		return proc.Spec{}, err
	}
	spec.LogPath = ""
	return spec, nil
}

// spec is the launch of the runtime's interpreter with args: the vendor's own recipe (its launcher puts the runtime directory in front of
// PATH and starts `runtime\python.exe -I <script>` in the package directory), the caches and temporary files below the runtime
// directory, and the catalog's environment that switches the automatic downloads of the libraries off.
func (i *Installer) spec(args, extraEnv []string) (proc.Spec, error) {
	l := i.Runtime.Launch
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
	env = append(env, extraEnv...)
	return proc.Spec{
		Command: i.PythonPath(), Args: args, Dir: i.Layout.EnvDir(), LogPath: i.Layout.LogPath(), Env: env, DropEnv: foreignEnv,
	}, nil
}
