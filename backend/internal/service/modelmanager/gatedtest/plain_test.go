package gatedtest_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// A model that is not behind the login keeps the plain message of a 401 / 403 (the source asks for a login); the guidance to apply
// for access belongs to the models that require the token.
func TestAModelWithoutTheLoginKeepsThePlainMessageOnA401Or403(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", status) }))
			t.Cleanup(site.Close)
			r := mmtest.New(t, withPlainModelAt(site.URL), mmtest.HFAuth())
			r.InstallEngine()

			st := r.InstallModel("m-plain")
			require.Equal(t, domain.JobFailed, st.State)
			assert.True(t, strings.HasPrefix(st.Error, "取得元が認証を求めています（"+strconv.Itoa(status)+"）。Hugging Face のトークンを設定してください: "), st.Error)
			for _, w := range []string{"アクセスを申請", "ライセンスに同意", hfguide.InvalidToken} {
				assert.NotContains(t, st.Error, w)
			}
			assert.Zero(t, r.HF.Count("/"), "Hugging Face was not asked")
		})
	}
}

// withPlainModelAt adds m-plain, a model that is not behind the login, whose one file is at base.
func withPlainModelAt(base string) mmtest.Option {
	return func(d *modelmanager.Deps, _ *mmtest.Rig) {
		d.Catalog.Models = append(d.Catalog.Models, catalog.Model{
			ID: "m-plain", Name: "P", Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, Description: "d",
			VRAM:      catalog.VRAM{MinGB: 8, Source: "test", Estimate: true},
			License:   catalog.License{ID: "plain-license", Version: "1", Name: "plain", URL: "https://example.com/plain", Summary: "s", CommercialUse: true, CommercialCondition: "c"},
			Files:     []catalog.File{{Name: "p.safetensors", Folder: "vae", URL: base + "/p.safetensors", SHA256: hftest.SHA([]byte("p")), Size: 1}},
			Workflows: []catalog.Workflow{{Name: "wf-p", Template: "txt2img-sdxl"}},
		})
	}
}
