// Package llmlocal holds the data of the card "ローカル LLM サーバー" of the category "テキスト生成（LLM）" of the settings screen: a link to the model manager's page "ローカル LLM" and the line
// that says where the calls of the provider "local" go now (#1104). The save and the connection test of the card moved there (handler/modelmgr/localllm); this package has no route. It is a
// package of its own because handler is at its size limit.
package llmlocal

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llmlocal"
)

// CardData is where the calls go now, as the card says it. Nothing is started.
func CardData(svc *llmlocal.Service) view.Data {
	st := svc.Status(context.Background())
	d := view.Data{State: st.Line()}
	if st.Problem() != "" {
		d.Problem = st.Word()
	}
	return d
}
