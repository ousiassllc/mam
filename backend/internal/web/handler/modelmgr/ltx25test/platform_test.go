package ltx25test_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// withPlatformColumns gives m-a the columns of an NVFP4 model (Blackwell only), m-b those of an fp8 model (not on macOS) and m-c both, the
// advanced one whose install asks for the license first.
func withPlatformColumns() mmtest.Option {
	return func(d *modelmanager.Deps, _ *mmtest.Rig) {
		req := &platform.Requires{GPUArch: []string{platform.ArchBlackwell}, Reason: "FP4 の演算が要ります。", Source: "test 2026-11-03"}
		uns := &platform.Unsupported{OS: []string{platform.OSDarwin}, Reason: "fp8 を MPS に載せられません。", Source: "test 2026-11-03"}
		for i := range d.Catalog.Models {
			switch m := &d.Catalog.Models[i]; m.ID {
			case "m-a":
				m.Requires = req
			case "m-b":
				m.Unsupported = uns
			case "m-c":
				m.Requires, m.Unsupported = req, uns
			}
		}
	}
}

// The OS / GPU generation columns of the catalog (Issue #979) warn on the screen of a PC that is outside them: a badge on the card, the sentence in the
// modal, a confirmation on the install button and the sentence again in the license dialog. A PC inside, or a model without the columns, is not
// warned, and the warning never takes the install button away.
func TestCatalog_ACardWarnsWhenThePCIsOutsideTheOSAndGPUGenerationOfTheModel(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s := newScreen(t, withPlatformColumns(), mmtest.WithClock(func() time.Time { return now }))
	host := func(goos string, adapters ...gpu.Adapter) string {
		s.rig.GPUReport.OS, s.rig.GPUReport.Adapters = goos, adapters
		now = now.Add(time.Hour) // the detection is remembered for a few minutes
		return s.get(paths.ModelManagerPart("models")).Body.String()
	}
	detail := func(id string) string { return s.get(paths.ModelManagerPart("models", id, "detail")).Body.String() }
	rtx := func(name string, cc float64) gpu.Adapter {
		return gpu.Adapter{Name: name, VendorID: gpu.VendorNVIDIA, VRAMBytes: 16 << 30, ComputeCap: cc}
	}

	// Windows with an RTX 4070 Ti SUPER (Ada): outside Blackwell, but a Windows PC is fine for fp8.
	body := host("windows", rtx("NVIDIA GeForce RTX 4070 Ti SUPER", 8.9))
	a := card(t, body, "m-a")
	assert.Contains(t, a, `data-testid="mm-platform-warning"`)
	assert.Contains(t, a, "この PC は対象外の見込み")
	assert.Contains(t, a, `data-testid="mm-install"`, "the install is not taken away")
	assert.Contains(t, a, "hx-confirm", "the install asks first")
	assert.Contains(t, a, "FP4 の演算が要ります。", "the confirmation says why")
	assert.NotContains(t, card(t, body, "m-b"), `data-testid="mm-platform-warning"`, "Windows is not unsupported")
	assert.NotContains(t, card(t, body, "m-b"), "hx-confirm")
	ad := detail("m-a")
	assert.Contains(t, ad, `data-testid="mm-platform-warning-text"`)
	assert.Contains(t, ad, "NVIDIA Ada Lovelace")
	assert.Contains(t, ad, "対象: NVIDIA Blackwell")
	assert.NotContains(t, detail("m-b"), "mm-platform-warning-text")

	// An RTX 5090: inside Blackwell.
	body = host("windows", rtx("NVIDIA GeForce RTX 5090", 12.0))
	assert.NotContains(t, card(t, body, "m-a"), `data-testid="mm-platform-warning"`)
	assert.NotContains(t, card(t, body, "m-a"), "hx-confirm")
	assert.NotContains(t, detail("m-a"), "mm-platform-warning-text")

	// A GPU whose generation cannot be told is not guessed about.
	body = host("windows", gpu.Adapter{Name: "NVIDIA Quadro Mystery", VendorID: gpu.VendorNVIDIA, VRAMBytes: 8 << 30})
	assert.NotContains(t, body, `data-testid="mm-platform-warning"`)

	// A Mac (the detector lists no adapter there): the fp8 model is unsupported, and the Blackwell one is outside.
	body = host("darwin")
	assert.Contains(t, card(t, body, "m-b"), `data-testid="mm-platform-warning"`)
	assert.Contains(t, card(t, body, "m-a"), `data-testid="mm-platform-warning"`)
	assert.Contains(t, detail("m-b"), "macOS")
	assert.Contains(t, detail("m-b"), "fp8 を MPS に載せられません。")

	// The license dialog of an advanced model shows the warning again where the install is started.
	consent := s.get(paths.ModelManagerPart("models", "m-c", "consent")).Body.String()
	assert.Contains(t, consent, `data-testid="mm-consent-platform-warning"`)
	assert.Contains(t, consent, "macOS")
	host("windows", rtx("NVIDIA GeForce RTX 5090", 12.0))
	assert.NotContains(t, s.get(paths.ModelManagerPart("models", "m-c", "consent")).Body.String(), `mm-consent-platform-warning`, "m-c is outside macOS only")
}
