package desktop

import (
	"os/exec"
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

// Updater is how the desktop front quits for an update and runs the installer once the server has
// stopped: the update service hands it a verified installer (Install), the front stops the server and
// then calls RunPending.
type Updater struct {
	quit  func()
	start func(*exec.Cmd) error

	mu      sync.Mutex
	pending string
}

// NewUpdater returns an Updater that asks the application to quit with quit (which must make the
// OnShutdown hook, and so RunPending, run).
func NewUpdater(quit func()) *Updater {
	return &Updater{quit: quit, start: (*exec.Cmd).Start}
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
// stopped: the installer replaces the exe of the running application, and waits for it to end.
func (u *Updater) RunPending() error {
	u.mu.Lock()
	path := u.pending
	u.mu.Unlock()
	if path == "" {
		return nil
	}
	return u.start(InstallCommand(path))
}
