package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserCommand_PerOS(t *testing.T) {
	const url = "http://127.0.0.1:8787"
	for goos, wantName := range map[string]string{"windows": "rundll32", "darwin": "open", "linux": "xdg-open"} {
		cmd, err := browserCommand(goos, url)
		require.NoError(t, err, goos)
		assert.Equal(t, url, cmd.Args[len(cmd.Args)-1])
		assert.Equal(t, wantName, filepath.Base(cmd.Args[0]), goos)
	}
	_, err := browserCommand("plan9", url)
	assert.Error(t, err)
}
