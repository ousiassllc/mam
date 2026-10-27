package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// silentInstallFlag is the NSIS flag that runs an installer without any page. The installer then
// restarts the app itself once it has installed (build/windows/nsis/project.nsi, .onInstSuccess).
const silentInstallFlag = "/S"

// InstallCommand builds, without starting it, the command that runs the verified installer silently.
// It is a function of its own because running a Windows installer cannot be tested on the Linux CI:
// what is tested is the command that is built.
func InstallCommand(installerPath string) *exec.Cmd {
	// No context: the installer must outlive this process, so nothing may ever kill it.
	cmd := exec.Command(installerPath, silentInstallFlag) // #nosec G204 -- the installer the updater downloaded and verified
	detach(cmd)
	return cmd
}

// installCommandFor is the command that installs the verified installer of the platform goos runs on: the
// NSIS installer on Windows (and wherever else, in tests), the dmg helper on macOS (MacInstallCommand), which
// needs this application's own bundle.
func installCommandFor(goos, installerPath string) (*exec.Cmd, error) {
	if goos != "darwin" {
		return InstallCommand(installerPath), nil
	}
	bundle, err := runningMacBundle()
	if err != nil {
		return nil, err
	}
	return MacInstallCommand(os.Getpid(), installerPath, bundle), nil
}

// runningMacBundle is the .app bundle this process runs from, when it can be replaced where it is.
func runningMacBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the running executable: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", fmt.Errorf("resolve the running executable: %w", err)
	}
	return MacUpdatableBundle(exe)
}

// CheckCanInstall reports why this process could not install an update where it runs, nil when it can (or when
// nothing here needs checking: Windows installs through its own installer). The front turns the automatic update
// off when it can not (macOS: running from a disk image or a read-only place), instead of finding out after the
// application has quit.
func CheckCanInstall() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	_, err := runningMacBundle()
	return err
}

// Updater is how the desktop front quits for an update and runs the installer once the server has
// stopped: the update service hands it a verified installer (Install), the front stops the server and
// then calls RunPending.
type Updater struct {
	quit    func()
	start   func(*exec.Cmd) error
	command func(installerPath string) (*exec.Cmd, error)

	mu      sync.Mutex
	pending string
}

// NewUpdater returns an Updater that asks the application to quit with quit (which must make the
// OnShutdown hook, and so RunPending, run).
func NewUpdater(quit func()) *Updater {
	return &Updater{
		quit:    quit,
		start:   (*exec.Cmd).Start,
		command: func(installerPath string) (*exec.Cmd, error) { return installCommandFor(runtime.GOOS, installerPath) },
	}
}

// Install remembers the installer and quits the application. It does not block: the update service
// calls it from the goroutine of the check, which the shutdown waits for, so quit runs on a goroutine
// of its own (quitting from the UI thread would wait for the thread that is waiting for it).
func (u *Updater) Install(installerPath string) {
	u.mu.Lock()
	u.pending = installerPath
	u.mu.Unlock()
	go u.quit()
}

// RunPending starts the installer Install remembered, detached from this process, and returns the
// error of starting it. It does nothing (nil) when no update is pending. Call it after the server
// stopped: the installer replaces the running application (the exe on Windows, the bundle on macOS), and waits
// for this process to end.
func (u *Updater) RunPending() error {
	u.mu.Lock()
	path := u.pending
	u.mu.Unlock()
	if path == "" {
		return nil
	}
	cmd, err := u.command(path)
	if err != nil {
		return err
	}
	return u.start(cmd)
}
