//go:build !windows

package media

import "os/exec"

// hideWindow is a no-op: only Windows GUI processes lack a console for a child to inherit.
func hideWindow(*exec.Cmd) {}
