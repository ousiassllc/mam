// Package choicetest gives the runtime of the lrtest catalog two models, for the tests of the layers that choose among them (the model manager's
// screen, the MCP tools). It is a package of its own because package lrtest is in a directory at its line budget.
package choicetest

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
)

// The models WithChoices gives the runtime: Small (the default; MIT), and Large whose license has to be accepted before it is chosen.
const (
	SmallChoice         = "small"
	LargeChoice         = "large"
	LargeConsentVersion = "terms-1"
	// LargeLicense is the id of the license Large is under (the "odd" license of the test catalog, which is not a commercial yes).
	LargeLicense = "odd"
	// LargeDest is where the weight of Large lies (it is only fetched while Large is in force).
	LargeDest = "models/large.bin"
)

// WithChoices gives the runtime of the test catalog two models: Small (the default, which "model" belongs to) and Large (its own weight, the
// "extra" file's bytes, and a license to accept). The weight of no model ("extra") is shared by both.
func WithChoices() lrtest.Option {
	return func(r *lrtest.Rig, d *localruntime.Deps) {
		set := &d.Catalog.ResourceSets[0]
		for i := range set.Assets {
			if set.Assets[i].Dest == lrtest.WeightDest {
				set.Assets[i].Choice = SmallChoice
			}
		}
		set.Assets = append(set.Assets, catalog.Resource{
			Name: "large", Description: "Large の重み", Dest: LargeDest, URL: r.Host.URL + lrtest.ExtraURLPath, SHA256: sum(lrtest.ExtraBytes), Size: int64(len(lrtest.ExtraBytes)),
			Required: true, Choice: LargeChoice, Source: catalog.ResourceSource{Name: "x", URL: "https://example.com/x", Note: "原本は x。2026-11-01 に確認した"}, License: LargeLicense,
		})
		set.Choices = []choice.Choice{
			{
				ID: SmallChoice, Name: "Small", Description: "小さいモデル", Default: true, Kind: "tts", Languages: []string{"ja"}, License: "mit",
				Cautions: []string{"本人の同意なく声をコピーしない"}, VRAMMinGB: 6, VRAMRecommendedGB: 8, VRAMLimits: "長い文で足りなくなることがある", VRAMNote: "mam の推定", VRAMEstimate: true,
			},
			{
				ID: LargeChoice, Name: "Large", Description: "大きいモデル", Kind: "tts", Languages: []string{"ja"}, License: LargeLicense,
				Consent:  &choice.Consent{Version: LargeConsentVersion, Text: "利用規約に同意します"},
				Cautions: []string{"禁止用途ポリシーに従う"}, VRAMMinGB: 10, VRAMNote: "mam の推定", VRAMEstimate: true,
				Env: []string{"IRODORI_MODEL_PRECISION=bf16"},
			},
		}
	}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
