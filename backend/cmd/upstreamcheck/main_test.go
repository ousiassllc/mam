package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_ListPrintsThePinsWithoutAskingTheUpstream(t *testing.T) {
	var out, errOut strings.Builder

	err := run(context.Background(), []string{"-list", "-only", "musetalk"}, func(string) string { return "" }, &out, &errOut)

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	assert.Greater(t, len(lines), 10)
	for _, l := range lines {
		cols := strings.Split(l, "\t")
		require.Len(t, cols, 4, l)
		assert.Regexp(t, `^[0-9a-f]{64}$`, cols[0])
		assert.True(t, strings.HasPrefix(cols[2], "https://"), l)
	}
}

func TestRun_RefusesAnUnknownFlag(t *testing.T) {
	var out, errOut strings.Builder

	err := run(context.Background(), []string{"-nope"}, func(string) string { return "" }, &out, &errOut)

	assert.Error(t, err)
}
