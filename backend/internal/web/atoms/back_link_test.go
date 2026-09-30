package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

func TestChatBackLink_LeadsToTheChatOfTheProjectWhateverItsIDHolds(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.ChatBackLink("a?b#c").Render(context.Background(), &sb))
	assert.Contains(t, sb.String(), `href="/projects/a%3Fb%23c"`)
	assert.Contains(t, sb.String(), "チャットに戻る")
}
