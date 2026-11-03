package realcatalogtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// #935 and #924: on a Mac (Apple silicon, 64 GiB of unified memory) the real catalog refuses every local runtime, and the refusal
// reaches the Audio Director as the typed error of its own provider, whose sentence for the user names the reason of that runtime
// and the cloud provider to use instead (never another runtime's). Nothing is installed and nothing falls back by itself.
func TestAMacRefusalReachesTheDirectorPerRuntimeWithItsOwnCloudProvider(t *testing.T) {
	apple := func(context.Context) gpu.Report {
		return gpu.Report{Adapters: []gpu.Adapter{{Name: "Apple M4 Max", VendorID: gpu.VendorApple, VRAMBytes: 64 << 30}}}
	}
	svc, err := localruntime.New(localruntime.Deps{
		Root: t.TempDir(), Platform: rtm.Platform{OS: "darwin", Arch: "arm64"}, GPU: apple, Queue: mmtest.New(t).Queue,
	})
	require.NoError(t, err)
	ctx := context.Background()

	for _, tc := range []struct {
		id, vendor string
		reason     []string
		cloud      string
		notOthers  []string
	}{
		{"gpt-sovits", "", []string{"GPT-SoVITS", "pyopenjtalk"}, "クラウドの声（ElevenLabs）", []string{"MuseTalk", "Irodori"}},
		{"musetalk", localvoice.VendorMuseTalk, []string{"MuseTalk 1.5", "mmcv 2.0.1", "mlx-community/MuseTalk-1.5-q4"}, "クラウドの口パク（Sync Labs）", []string{"GPT-SoVITS", "Irodori"}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			for name, call := range map[string]func() error{
				"install": func() error { _, err := svc.Install(ctx, tc.id); return err },
				"start":   func() error { _, err := svc.Start(ctx, tc.id); return err },
			} {
				err := call()
				require.ErrorIs(t, err, localruntime.ErrUnsupported, name)

				var un *localvoice.UnavailableError
				require.ErrorAs(t, localspeech.UnavailableFor(ctx, tc.vendor, err), &un, name)
				assert.Equal(t, localvoice.KindUnsupported, un.Kind, name)
				assert.True(t, un.Systemic(), "the whole runtime is out: the next line fails the same way")
				text := localvoice.Reason(un)
				for _, s := range tc.reason {
					assert.Contains(t, text, s, name)
				}
				assert.Contains(t, text, tc.cloud, name)
				for _, s := range tc.notOthers {
					assert.NotContains(t, text, s, name)
				}
			}
		})
	}
}
