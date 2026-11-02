package nodetools_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr/nodetools"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes/nodetest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

func newEnv(t *testing.T) (*mcptest.Env, *mmtest.Rig, *nodetest.Rig) {
	t.Helper()
	var mm *mmtest.Rig
	var nr *nodetest.Rig
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		mm = mmtest.New(t, mmtest.WithDB(env.DB), mmtest.WithLogger(env.Logger))
		nr = nodetest.New(t, nodetest.Shared(mm.Queue, mm.Table, mm.Svc.HandleInstall))
		env.Services.ModelManager, env.Services.ModelNodes = mm.Svc, nr.Svc
		jobs.Register(r, env.Services)
		modelmgr.Register(r, env.Services)
	})
	return env, mm, nr
}

func acceptArgs(v modelnodes.View, id string) map[string]any {
	return map[string]any{"node_id": id, "license_id": v.License.ID, "license_version": v.License.Version, "notice_version": v.NoticeVersion}
}

func TestListShowsThePinsTheLicenseAndWhyANodeIsNotInstalled(t *testing.T) {
	env, _, _ := newEnv(t)
	var list nodetools.ListOutput
	env.CallOK("list_custom_nodes", map[string]any{}, &list)
	require.Len(t, list.Nodes, 2)
	pack := list.Nodes[0]
	assert.Equal(t, "pack", pack.ID)
	assert.Equal(t, nodetest.Commit, pack.Commit)
	assert.Regexp(t, `^[0-9a-f]{64}$`, pack.ArchiveSHA)
	assert.Equal(t, "GPL-3.0", pack.License.Name)
	assert.NotEmpty(t, pack.SecurityNotice)
	assert.Equal(t, "not_installed", string(pack.State))
	blocked := list.Nodes[1]
	assert.Equal(t, "blocked", string(blocked.State))
	assert.Contains(t, blocked.InstallBlocked, "opencv")

	assert.Contains(t, env.CallError("get_custom_node_status", map[string]any{"node_id": "nope"}), "not_found")
}

// The tools never accept on the user's behalf: an install without a record of the acceptance is refused, as is a record of another version; the node
// is installed only after the acceptance, and get_job reads the job as one of a node.
func TestInstallNeedsTheAcceptanceThenRunsAsAJobAndRemoveUndoesIt(t *testing.T) {
	env, mm, nr := newEnv(t)
	assert.Contains(t, env.CallError("install_custom_node", map[string]any{"node_id": "pack"}), "同意してから")
	assert.Empty(t, nr.Nodes.Enabled())

	var v modelnodes.View
	env.CallOK("get_custom_node_status", map[string]any{"node_id": "pack"}, &v)
	bad := acceptArgs(v, "pack")
	bad["notice_version"] = "old"
	assert.Contains(t, env.CallError("accept_custom_node_license", bad), "validation")

	var acc nodetools.AcceptOutput
	env.CallOK("accept_custom_node_license", acceptArgs(v, "pack"), &acc)
	assert.True(t, acc.Accepted)

	var out nodetools.JobOutput
	env.CallOK("install_custom_node", map[string]any{"node_id": "pack"}, &out)
	assert.Equal(t, "node:pack", out.Target)
	mm.Start()
	var st mcpkit.JobStatus
	require.Eventually(t, func() bool {
		env.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
		return st.State.Final()
	}, 30e9, 20e6)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "node:pack", st.Result["target"])
	assert.Equal(t, []string{"pack"}, nr.Nodes.Enabled())

	var removed nodetools.RemovedOutput
	env.CallOK("remove_custom_node", map[string]any{"node_id": "pack"}, &removed)
	assert.True(t, removed.Removed)
	assert.Empty(t, nr.Nodes.Enabled())
	assert.Contains(t, env.CallError("remove_custom_node", map[string]any{"node_id": "pack"}), "conflict")
}

func TestANodeMamDoesNotInstallIsRefusedWithTheReason(t *testing.T) {
	env, _, nr := newEnv(t)
	msg := env.CallError("install_custom_node", map[string]any{"node_id": "needs-pip"})
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, "opencv")
	assert.Empty(t, nr.Nodes.Enabled())
}

func TestWithoutANodeManagerTheToolsAreUnavailable(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { nodetools.Register(r, env.Services) })
	assert.Contains(t, env.CallError("list_custom_nodes", map[string]any{}), "unavailable")
}
