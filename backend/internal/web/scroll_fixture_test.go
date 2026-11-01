package web_test

import (
	"strconv"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// The wtest static/src/components/scroll/scroll.wtest.ts draws the model manager from these files and checks that the page
// stays where the reader is when the install button is pressed and while the list polls (#908). They are the real screen and
// the real answers of the handlers (backend/internal/web/handler/modelmgr/models.go), so the test measures the real markup:
// `MAM_UPDATE_FIXTURES=1 go test ./backend/internal/web -run TestScrollFixtures` writes them again.
const (
	scrollModels = 8
	consentModel = 8
)

func scrollModel(i int, state string, job *modelmgr.Progress) modelmgr.Model {
	id := "model-" + strconv.Itoa(i)
	m := modelmgr.Model{
		ID: id, Name: "モデル " + strconv.Itoa(i), KindLabel: "画像", Description: "試験用のモデルです。説明は 2 行ほどになる長さにします。説明は 2 行ほどになる長さにします。",
		Size: "6.5 GiB", Need: "6.5 GiB", VRAM: "12 GiB 以上",
		License:  modelmgr.License{ID: "apache-2.0", Version: "1", Name: "Apache-2.0", URL: "https://example.com/license", Commercial: "可"},
		State:    state,
		Files:    []modelmgr.File{{Path: "checkpoints/" + id + ".safetensors", Size: "6.5 GiB"}},
		ModelIDs: []string{id}, WorkflowCheck: "ワークフロー確認済み",
	}
	m.Install = job
	// The last model needs the acceptance of its license: its button opens the dialog (consentModel).
	if i == consentModel {
		m.Advanced, m.Consent, m.Accepted = true, true, state != "not_installed"
	}
	return m
}

// scrollCatalog is the list with the model `installing` (running when there are jobs, done when there are none; 0: none).
func scrollCatalog(installing int, jobs []string) modelmgr.Catalog {
	c := modelmgr.Catalog{Jobs: jobs}
	for i := 1; i <= scrollModels; i++ {
		switch {
		case i == installing && len(jobs) > 0:
			c.Models = append(c.Models, scrollModel(i, "installing", &modelmgr.Progress{JobID: jobs[0], Percent: 3, Message: "ダウンロード中", Bytes: "0.2 GiB / 6.5 GiB"}))
		case i == installing:
			c.Models = append(c.Models, scrollModel(i, "installed", nil))
		default:
			c.Models = append(c.Models, scrollModel(i, "not_installed", nil))
		}
	}
	return c
}

func scrollTrial(oob bool) modelmgr.TrialPanel {
	p := modelmgr.TrialPanel{OOB: oob, Form: modelmgr.TrialForm{Media: "image", MaxPrompt: 2000, Aspect: "16:9", Models: []modelmgr.TrialModel{{ID: "model-1", Label: "モデル 1", Selected: true}}}}
	for i := 1; i <= 3; i++ {
		p.Trials.Items = append(p.Trials.Items, modelmgr.Trial{ID: "t" + strconv.Itoa(i), ModelID: "model-1", Prompt: "試し生成 " + strconv.Itoa(i), Status: "succeeded", StatusText: "完了", CreatedAt: "2026-11-01 10:00", Deletable: true})
	}
	return p
}

func TestScrollFixtures_AreWhatTheComponentsRender(t *testing.T) {
	started := scrollCatalog(5, []string{"job-1"})
	started.Notice = "導入を始めました。この画面を閉じても続きます"
	finished := scrollCatalog(5, nil)
	finished.Notice = "導入が終わりました"

	consent := modelmgr.Consent{
		ModelID: "model-" + strconv.Itoa(consentModel), Name: "モデル " + strconv.Itoa(consentModel),
		License: modelmgr.License{ID: "custom", Version: "1", Name: "独自ライセンス", URL: "https://example.com/license", Summary: "利用条件の要約です。", Commercial: "条件付き", Regions: "一部の地域では使えません。", Notice: "ライセンスの全文を読んでください。"},
		Sources: []string{"example.com"},
	}
	startedConsent := scrollCatalog(consentModel, []string{"job-2"})
	startedConsent.Notice = started.Notice

	checkFixtures(t, map[string]templ.Component{
		"scroll-mm-consent.html":        modelmgr.ConsentDialog(consent),
		"scroll-mm-consent-answer.html": modelmgr.CatalogWithDialogClear(startedConsent),
		"scroll-mm-page.html":           modelmgr.Body(modelmgr.Data{Catalog: scrollCatalog(0, nil), Trial: scrollTrial(false), HFImport: modelmgr.HFImport{Kind: "text-to-image"}}),
		"scroll-mm-install-answer.html": modelmgr.CatalogWithDialogClear(started),
		"scroll-mm-poll-done.html":      modelmgr.CatalogWithTrial(finished, scrollTrial(true)),
	})
}
