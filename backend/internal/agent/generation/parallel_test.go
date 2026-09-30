package generation

import (
	"bytes"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParallel_APanickingTaskBecomesItsErrorAndTheOthersFinish(t *testing.T) {
	var logs bytes.Buffer
	var ran atomic.Int32

	errs := parallel(slog.New(slog.NewTextHandler(&logs, nil)), 4, func(i int) error {
		ran.Add(1)
		switch i {
		case 1:
			panic("make take exploded")
		case 2:
			return errors.New("plain failure")
		}
		return nil
	})

	require.Len(t, errs, 4)
	assert.EqualValues(t, 4, ran.Load(), "every task ran")
	assert.NoError(t, errs[0])
	require.Error(t, errs[1])
	assert.Contains(t, errs[1].Error(), "make take exploded")
	assert.EqualError(t, errs[2], "plain failure")
	assert.NoError(t, errs[3])
	assert.Contains(t, logs.String(), "parallel task panicked")
	assert.Contains(t, logs.String(), "stack=")
}
