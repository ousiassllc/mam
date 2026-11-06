package ollama

import (
	"context"
	"strings"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/jobview"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/ollamacard"
)

// maxJobs bounds the jobs a request names (the query ?jobs=): a page has at most one pull per model.
const maxJobs = 8

// Section is the section of the page "ローカル LLM": the state of the Ollama of mam and a card for every model of the catalog. Nil (the section is left out) without the service.
func Section(ctx context.Context, svc *ollamapull.Service) templ.Component {
	if svc == nil {
		return nil
	}
	return card.OllamaSection(sectionData(ctx, svc, nil))
}

// SectionOOB is the section drawn out of band into the page that has it: what an action of the runtime card ("Ollama") sends along, because installing or starting the server
// changes what the pull buttons can do. Nil without the service.
func SectionOOB(ctx context.Context, svc *ollamapull.Service) templ.Component {
	if svc == nil {
		return nil
	}
	s := sectionData(ctx, svc, nil)
	s.OOB = true
	return card.OllamaSection(s)
}

// sectionData reads the section now. jobs are the pulls the caller reports on: one that has ended is told on its card (how it ended) once, one the queue has not recorded the end
// of yet is shown as running.
func sectionData(ctx context.Context, svc *ollamapull.Service, jobs []string) card.Section {
	v := svc.List(ctx)
	s := card.Section{Server: string(v.Server), Reason: v.Reason}
	index := map[string]int{}
	for _, mv := range v.Models {
		m := modelCard(mv, v.Server)
		if mv.PullJob != "" {
			m.Install = progress(ctx, svc, mv.PullJob)
			s.Jobs = append(s.Jobs, mv.PullJob)
		}
		index[mv.Model.Name] = len(s.Items)
		s.Items = append(s.Items, m)
	}
	for _, id := range jobs {
		st, err := svc.JobStatus(ctx, id)
		i, ok := index[strings.TrimPrefix(st.Target, ollamapull.TargetPrefix)]
		if err != nil || !ok || s.Items[i].Install != nil {
			continue
		}
		m := &s.Items[i]
		if st.State == domain.JobQueued || st.State == domain.JobRunning {
			m.State, m.Install = card.StatePulling, progress(ctx, svc, id) // the handler is done, the queue has not recorded the end yet
			s.Jobs = append(s.Jobs, id)
			continue
		}
		notice, f := jobview.Ending(st.State, st.Error, st.Failure, m.DisplayName+" を取得しました", m.DisplayName+" の取得に失敗しました")
		jobview.Settle(&m.Message, notice, f)
	}
	for _, o := range v.Others {
		s.Others = append(s.Others, card.Other{Name: o.Name, Size: jobview.BytesText(o.Size)})
	}
	return s
}

func modelCard(mv ollamapull.ModelView, server ollamapull.Server) card.Model {
	m := mv.Model
	c := card.Model{
		Name: m.Name, DisplayName: m.DisplayName, Description: m.Description, Size: "約 " + jobview.BytesText(m.TotalBytes), State: string(mv.State),
		Commercial: commercial.FromInfo(m.License), NoSpace: mv.NoSpace && mv.State == ollamapull.StateMissing,
	}
	if mv.State == ollamapull.StateUnavailable || mv.State == ollamapull.StateMissing {
		c.Blocked = card.BlockedReason(string(server))
	}
	return c
}

// detailData is the modal of a model: what the registry's manifest recorded, and what the check of the manifest on disk says.
func detailData(svc *ollamapull.Service, m ollamamodels.Model) card.Detail {
	d := card.Detail{
		Name: m.Name, DisplayName: m.DisplayName, Description: m.Description, Commercial: commercial.FromInfo(m.License), Size: jobview.BytesText(m.TotalBytes),
		Digest: m.ModelLayer().Digest, CheckedAt: m.CheckedAt, LicenseName: m.License.LicenseName, LicenseURL: m.License.SourceURL, LicenseSummary: m.LicenseSummary,
		SourceURL: "https://ollama.com/library/" + m.Name,
	}
	for _, l := range m.Layers {
		d.Layers = append(d.Layers, card.Layer{Kind: l.Kind, Size: jobview.BytesText(l.Size), Digest: l.Digest})
	}
	switch svc.CheckManifest(m) {
	case ollamapull.CheckVerified:
		d.Check = "取得済みで、Ollama が書いた内容は、確認した時点の記録と一致しています。"
	case ollamapull.CheckChanged:
		d.Check, d.Changed = "取得済みですが、Ollama が書いた内容が記録と違います。取得元のタグが書き換わったか、別の手段で取得した内容です。そのまま使えますが、内容は固定されていません。", true
	}
	return d
}

// progress is what a running pull reports (a placeholder while its status cannot be read).
func progress(ctx context.Context, svc *ollamapull.Service, jobID string) *view.Progress {
	st, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		return &view.Progress{JobID: jobID, Message: "状態を取得しています"}
	}
	p := &view.Progress{JobID: jobID, Percent: st.Progress.Percent, Message: st.Progress.Message}
	if st.State == domain.JobQueued {
		p.Message = "順番を待っています"
	}
	if p.Message == "" {
		p.Message = "準備しています"
	}
	if t := st.Progress.TotalBytes; t > 0 {
		p.Bytes = jobview.BytesText(st.Progress.DoneBytes) + " / " + jobview.BytesText(t)
	}
	return p
}

// jobIDs is the pulls a request names (?jobs=a,b), at most maxJobs.
func jobIDs(q string) []string {
	var ids []string
	for _, id := range strings.Split(q, ",") {
		if id = strings.TrimSpace(id); id != "" && len(ids) < maxJobs {
			ids = append(ids, id)
		}
	}
	return ids
}
