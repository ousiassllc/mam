package nodes

import (
	"context"
	"fmt"
	"strings"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/jobview"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/nodecard"
)

// Section is the section of the screen: a card for every node of the allowlist. Nil (the section is left out) when there is no service.
func Section(ctx context.Context, svc *modelnodes.Service) templ.Component {
	if svc == nil {
		return nil
	}
	return card.NodesSection(sectionData(ctx, svc))
}

func sectionData(ctx context.Context, svc *modelnodes.Service) card.Section {
	var s card.Section
	for _, v := range svc.List(ctx) {
		s.Items = append(s.Items, nodeCard(ctx, svc, v, ""))
	}
	return s
}

// nodeData is the card of one node. job is the install job it reports on ("" for the running one).
func nodeData(ctx context.Context, svc *modelnodes.Service, id, job string) (card.Node, error) {
	v, err := svc.Status(ctx, id)
	if err != nil {
		return card.Node{}, err
	}
	return nodeCard(ctx, svc, v, job), nil
}

func nodeCard(ctx context.Context, svc *modelnodes.Service, v modelnodes.View, job string) card.Node {
	n := card.Node{
		ID: v.ID, Name: v.Name, Description: v.Description, Repo: v.Repo, Commit: v.Commit, ArchiveSHA: v.ArchiveSHA,
		Size: jobview.BytesText(v.ArchiveSize), Extracted: jobview.BytesText(v.ExtractedSize), Need: jobview.BytesText(v.NeedBytes),
		LicenseID: v.License.ID, LicenseVersion: v.License.Version, LicenseName: v.License.Name, LicenseURL: v.License.URL,
		LicenseSummary: v.License.Summary, LicenseCommercial: commercialText(v), LicenseNotice: v.License.Notice,
		Commercial: commercial.FromInfo(v.License.Info()), CheckedAt: v.License.CheckedAt,
		PythonRequires: join(v.PythonRequires, "（なし）"), PythonOptional: join(v.PythonOptional, ""), Limits: v.Limits,
		Classes: join(v.NodeClasses, "（なし）"), ReviewedAt: v.ReviewedAt, Review: v.Review,
		State: string(v.State), Detail: v.Detail, Blocked: v.InstallBlocked, InstalledCommit: v.InstalledCommit,
		Unavailable: v.Unavailable, Accepted: v.Accepted, NoticeVersion: modelnodes.NoticeVersion, SecurityNotice: v.SecurityNotice,
	}
	if v.InstalledAt != nil {
		n.InstalledAt = v.InstalledAt.Local().Format("2006-01-02 15:04")
	}
	for _, m := range v.Models {
		n.Models = append(n.Models, card.Model{Name: m.Name, Installed: m.Installed})
	}
	switch {
	case v.InstallJob != "":
		n.Install, n.Job = progress(ctx, svc, v.InstallJob), v.InstallJob
	case job != "":
		if st, err := svc.JobStatus(ctx, job); err == nil && (st.State == domain.JobQueued || st.State == domain.JobRunning) {
			n.Install, n.Job = progress(ctx, svc, job), job // the handler is done, the queue has not recorded the end yet
		} else {
			n.Notice, n.Error = ended(ctx, svc, job, v.Name)
		}
	}
	return n
}

func join(items []string, none string) string {
	if len(items) == 0 {
		return none
	}
	return strings.Join(items, "、")
}

// commercialText is the answer for commercial use in words (the same wording as the license of a model).
func commercialText(v modelnodes.View) string {
	l := v.License
	switch {
	case l.Commercial == modellicense.Conditional && l.CommercialCondition != "":
		return "条件つきで可（" + l.CommercialCondition + "）"
	case l.Commercial == modellicense.Conditional:
		return "条件つきで可"
	case l.Commercial == modellicense.Yes && l.CommercialCondition != "":
		return "可（" + l.CommercialCondition + "）"
	case l.Commercial == modellicense.Yes:
		return "可"
	case l.Commercial == modellicense.Unknown:
		return "不明（確認できていません）"
	case l.CommercialCondition != "":
		return "不可（" + l.CommercialCondition + "）"
	}
	return "不可（非商用）"
}

// progress is what a running install reports (a placeholder while its status cannot be read).
func progress(ctx context.Context, svc *modelnodes.Service, jobID string) *view.Progress {
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

// ended is how an install job ended, as a notice or an error line; both are empty while it runs (or when it is unknown).
func ended(ctx context.Context, svc *modelnodes.Service, jobID, name string) (notice, failure string) {
	st, err := svc.JobStatus(ctx, jobID)
	if err != nil {
		return "", ""
	}
	switch st.State {
	case domain.JobSucceeded:
		return name + " を導入しました", ""
	case domain.JobFailed:
		reason := st.Error
		if reason == "" {
			reason = "原因は mam のログを確認してください"
		}
		return "", fmt.Sprintf("%s の導入に失敗しました: %s", name, strings.TrimSpace(reason))
	}
	return "", ""
}
