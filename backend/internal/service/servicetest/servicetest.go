// Package servicetest builds the services for tests.
package servicetest

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// New is service.New that stops the background workers of the services (Services.Close) when the
// test ends. Call it after registering the cleanup that closes the database: cleanups run last in,
// first out, so the workers stop before the database closes.
func New(t testing.TB, deps service.Deps) *service.Services {
	t.Helper()
	svc := service.New(deps)
	t.Cleanup(svc.Close)
	return svc
}
