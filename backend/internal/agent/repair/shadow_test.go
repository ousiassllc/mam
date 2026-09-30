package repair_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// panickyShadow is a Jev client that panics when asked.
type panickyShadow struct{}

func (panickyShadow) Choose(context.Context, jev.Input) (jev.Choice, error) {
	panic("jev exploded")
}

func (panickyShadow) Model() string { return "jev-test" }

func TestGoShadow_APanicInTheMeasurementIsLoggedAndDoesNotEscapeTheGoroutine(t *testing.T) {
	var logs bytes.Buffer
	a := repair.New(repair.Deps{Shadow: panickyShadow{}, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	var done sync.WaitGroup

	a.GoShadow(context.Background(), &done, "action-1", domain.JudgeResult{}, nil, domain.RepairFullRetake)
	done.Wait() // returns: the goroutine ended without crashing the test process

	assert.Contains(t, logs.String(), "repair shadow panicked")
	assert.Contains(t, logs.String(), "jev exploded")
	assert.Contains(t, logs.String(), "action-1")
	assert.Contains(t, logs.String(), "stack=")
}
