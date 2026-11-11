package flowtests_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// The confirmation card of the budget in the first message is a second write after the Project is created. When it fails the Project is still there and Create does not fail
// (a retry would create a second Project); the thread says that nothing was asked, and no ceiling is set.
func TestCreate_AFailedBudgetCardLeavesTheProjectAndSaysSo(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER fail_operation_card BEFORE INSERT ON chat_message WHEN NEW.ref_type = 'operation' BEGIN SELECT RAISE(ABORT, 'injected'); END`)
		return err
	}))

	p, err := f.svc.Projects.Create(ctx, service.CreateProjectInput{Content: "30秒の縦型のCMを作りたい。予算は$20で"})

	require.NoError(t, err, "the Project was created: an error would invite a retry that creates a second one")
	list, err := f.svc.Projects.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
	thread := f.thread(t, p.ID)
	assert.Empty(t, operationCards(thread))
	last := lastMessage(t, thread)
	assert.Equal(t, domain.RoleAssistant, last.Role)
	assert.Contains(t, last.Content, "予算の確認カードを出せませんでした")
	project, _ := ceilings(t, f.svc, p.ID)
	assert.Nil(t, project, "no ceiling is set without the user's confirmation")
}
