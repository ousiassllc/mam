package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/service"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestHealthService_Check(t *testing.T) {
	assert.Equal(t, service.HealthStatus{OK: true, Database: "ok"},
		service.NewHealthService(fakePinger{}).Check(context.Background()))
	assert.Equal(t, service.HealthStatus{OK: false, Database: "unavailable"},
		service.NewHealthService(fakePinger{err: errors.New("locked")}).Check(context.Background()))
}

type memStore map[string]string

func (m memStore) Set(p, k string) error { m[p] = k; return nil }
func (m memStore) Get(p string) (string, error) {
	if k, ok := m[p]; ok {
		return k, nil
	}
	return "", credential.ErrNotFound
}
func (m memStore) Delete(p string) error {
	if _, ok := m[p]; !ok {
		return credential.ErrNotFound
	}
	delete(m, p)
	return nil
}
func (m memStore) Has(p string) (bool, error) { _, ok := m[p]; return ok, nil }

func TestCredentialService_SaveConfiguredRemove(t *testing.T) {
	store := memStore{}
	svc := service.NewCredentialService(store)

	ok, err := svc.Configured("kling")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, svc.Save("kling", "sk-1"))
	ok, err = svc.Configured("kling")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "sk-1", store["kling"])

	require.NoError(t, svc.Remove("kling"))
	assert.NoError(t, svc.Remove("kling"), "removing a missing key is idempotent")
	ok, _ = svc.Configured("kling")
	assert.False(t, ok)
}
