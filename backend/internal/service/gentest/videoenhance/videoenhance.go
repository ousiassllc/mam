// Package videoenhance is "make a video larger" and "make a video smoother" of the generation test (Issue #1031, spec/architecture/gentest/gentest.md §3.9):
// the models that do it (the local workflows SeedVR2 and RIFE, and the paid cloud Topaz Video API), the estimate of the cloud and the start of an
// enhancement of a succeeded video result as a batch of its own with one result. It is a package of its own so that package gentest stays under
// the line limit; it uses the service only through its exported methods and Deps.
//
// The local models are free and start at once. A cloud model costs credits: it starts only with the estimate (EstimateID) the user confirmed
// (ConfirmedCredits equal to the quote), and a quote runs once.
package videoenhance

import (
	"context"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// CloudPrefix starts the id of a model of the cloud as it is listed (the result's model column holds it too): topaz:<id of Topaz>.
const CloudPrefix = "topaz:"

// topazLicense is the answer of a Topaz model whose provider gave none: the documentation (FAQ) asks for the credit and says nothing of the rights
// to the output, so the answer is "unknown" with the source (never a guess).
var topazLicense = modellicense.Info{
	Commercial:  modellicense.Unknown,
	LicenseName: "Topaz Labs API 利用規約（出力の権利は公式ドキュメントに記載なし）",
	SourceURL:   "https://developer.topazlabs.com/getting-started/faqs",
	CheckedAt:   "2026-11-04",
	Notice:      "公式ドキュメント（FAQ）には、出力した動画の権利や商用利用の可否が書かれていません。商用で使う前に Topaz Labs の利用規約で確かめてください。「Powered by Topaz Labs」の表示が必要です。",
}

// Enhancers lists the models that make a video larger or smoother: the local workflows of the running setting, then the models of the cloud (when the
// app has one) with whether each can be used now. A cloud model whose API key is not registered is listed as unavailable, with the words of #1013.
func Enhancers(_ context.Context, g *gentest.Service) []modelcatalog.VideoEnhancer {
	cat := modelcatalog.Catalog{Adapter: g.Adapter, Hidden: g.Hidden}
	out := cat.VideoEnhancers()
	if g.Cloud == nil {
		return out
	}
	configured := g.Cloud.Configured()
	for _, m := range g.Cloud.Models() {
		e := modelcatalog.VideoEnhancer{
			ID: CloudPrefix + m.ID, Op: string(m.Op), Label: m.Label, Description: m.Description, Provider: string(m.Provider), Paid: true,
			Available: configured, License: m.License, Attribution: venhance.TopazAttribution,
		}
		if e.License.LicenseName == "" {
			e.License = topazLicense
		}
		if !configured {
			e.Unavailable, e.UnavailableMessage = modelcatalog.UnavailableNoKey, "Topaz の"+modelcatalog.NoKeyMessage
		}
		out = append(out, e)
	}
	return out
}

// isCloud says an id is the id of a model of the cloud.
func isCloud(id string) bool { return strings.HasPrefix(id, CloudPrefix) }

// pick is the model named for op, or the first local one that can run now when none is named (a model that costs is never picked for the user).
func pick(list []modelcatalog.VideoEnhancer, op cloudenhance.Op, id string) (modelcatalog.VideoEnhancer, error) {
	if id == "" {
		i := slices.IndexFunc(list, func(e modelcatalog.VideoEnhancer) bool { return e.Op == string(op) && !e.Paid && e.Available })
		if i < 0 {
			return modelcatalog.VideoEnhancer{}, invalid("%sに使えるモデルが入っていません。モデルマネージャーで動画の%s用のモデルを入れてください（Topaz を使うときは、設定で API キーを入れて model に topaz:<モデル名> を指定します）",
				venhance.OpName(op), venhance.OpName(op))
		}
		return list[i], nil
	}
	i := slices.IndexFunc(list, func(e modelcatalog.VideoEnhancer) bool { return e.ID == id })
	switch {
	case i < 0:
		return modelcatalog.VideoEnhancer{}, invalid("モデル %q はありません（list_generation_video_enhancers の id を指定してください）", id)
	case list[i].Op != string(op):
		return modelcatalog.VideoEnhancer{}, invalid("モデル %q は動画の%sはできません（%s用のモデルです）", id, venhance.OpName(op), venhance.OpName(cloudenhance.Op(list[i].Op)))
	case !list[i].Available:
		return modelcatalog.VideoEnhancer{}, invalid("モデル %q は今は使えません: %s", id, list[i].UnavailableMessage)
	}
	return list[i], nil
}
