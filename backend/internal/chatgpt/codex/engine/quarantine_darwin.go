package engine

import "golang.org/x/sys/unix"

// errNoAttr is what removexattr reports for a file without the attribute.
var errNoAttr error = unix.ENOATTR

func removeXattr(path, attr string) error { return unix.Removexattr(path, attr) }
