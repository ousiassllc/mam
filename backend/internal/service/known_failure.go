package service

import "github.com/ousiassllc/mam/backend/internal/apperr/errmap"

// isKnownFailure reports whether errmap can tell err (a failure of an outside service, the disk or the network): the user then reads its
// cause and remedy (#1010).
func isKnownFailure(err error) bool {
	_, ok := errmap.Known(err)
	return ok
}
