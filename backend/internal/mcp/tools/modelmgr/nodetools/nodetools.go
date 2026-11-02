// Package nodetools holds the MCP tools of the custom nodes of the bundled ComfyUI (the section "追加ノード" of the model manager screen):
// list_custom_nodes, get_custom_node_status, accept_custom_node_license, install_custom_node and remove_custom_node
// (spec/api/mcp-tools-model-manager.md, spec/architecture/model-manager/custom-nodes.md). It is a package of its own because the modelmgr
// package is at its size limit. The tools call the same modelnodes.Service as the screen, so the same checks hold: only a node of the allowlist,
// only with the acceptance of its license and of the security notice on record (the tools never accept on the user's behalf), only on the
// machine with the bundled engine. An install is a job (get_job reads it through the Probe of modelmgr, which asks JobProbe).
package nodetools

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
)

// Register adds the tools. They answer "unavailable" when the app has no node manager (svc.ModelNodes is nil).
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc, registrar: r}
	mcpkit.Read(r, "list_custom_nodes",
		"ComfyUI に足せる追加ノード（カスタムノード）の許可リスト。各ノードの状態（not_installed・installed・outdated・modified・blocked）、固定したコミットとアーカイブの SHA-256、ライセンス・商用利用の可否（yes / conditional / no / unknown。unknown は notice を必ず読む）、必要な Python パッケージ、ソースの確認結果（review）、導入できない理由（install_blocked）、このノードを使うモデル、導入の前にユーザーが同意する注意事項（security_notice）を返す。許可リストにないノードは、フォルダを置いても読み込まれない / The allowlist of custom nodes with their pins, license and commercial use, review and state.",
		h.list)
	mcpkit.Read(r, "get_custom_node_status",
		"追加ノード 1 つの状態。list_custom_nodes と同じ内容 / State of one custom node.",
		h.status)
	mcpkit.Write(r, "accept_custom_node_license",
		"追加ノードのライセンスと注意事項（security_notice: ノードは ComfyUI の中でそのまま実行されるプログラム）に同意したことを記録する。license_id・license_version・notice_version は get_custom_node_status が返した値で、違えば拒否される。ユーザーが内容を確認して同意した後にだけ呼ぶ / Record that the user accepted the license and the security notice of a node.",
		h.accept, mcpkit.Idempotent())
	mcpkit.Write(r, "install_custom_node",
		"追加ノードを固定のコミットの GitHub のアーカイブから取得して導入する（SHA-256 を検証し、pip は実行しない）。動いている ComfyUI は一度止めて再起動し、ノードが読み込まれたことを確かめる。job_id を返すので get_job で進捗を見る。先に accept_custom_node_license しないと拒否される（API が確かめる）。許可リストにないノード・導入できないノード（install_blocked）・同梱の ComfyUI がない環境・空き容量不足も、始めずに拒否される / Download, verify and install a node as a job (get_job); the acceptance has to be on record.",
		h.install, mcpkit.OpenWorld())
	mcpkit.Destroy(r, "remove_custom_node",
		"導入した追加ノードを削除する（動いている ComfyUI は一度止めて再起動する）。このノードを使うモデルは、導入し直すまで選べなくなる。導入中は拒否される / Delete an installed node; the models that need it cannot be chosen until it is installed again.",
		h.remove, mcpkit.Idempotent())
}

type handlers struct {
	svc       *service.Services
	registrar *mcpkit.Registrar
}

func (h handlers) nodes() (*modelnodes.Service, error) {
	if h.svc.ModelNodes == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "追加ノードの管理はこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.ModelNodes, nil
}

// MapError turns the refusals of the node service into the errors the tools map: an input that cannot be accepted is a validation error, a state that
// does not allow it (the engine missing, an install running, a node not installed) a conflict, an unknown node not found.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, modelmanager.ErrNotInstalled) {
		return &service.ConflictError{Message: "導入されていません"}
	}
	kind, msg := mmview.Refuse(err)
	switch kind {
	case mmview.RefusalInvalid:
		return &service.ValidationError{Message: msg}
	case mmview.RefusalConflict:
		return &service.ConflictError{Message: msg}
	case mmview.RefusalNotFound:
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: msg, Status: http.StatusNotFound}
	}
	return err
}

// NodeInput names a node.
type NodeInput struct {
	NodeID string `json:"node_id" jsonschema:"list_custom_nodes の id（ipadapter-plus など）"`
}

// ListOutput is the allowlist.
type ListOutput struct {
	Nodes []modelnodes.View `json:"nodes"`
}

func (h handlers) list(ctx context.Context, _ struct{}) (ListOutput, error) {
	s, err := h.nodes()
	if err != nil {
		return ListOutput{}, err
	}
	return ListOutput{Nodes: s.List(ctx)}, nil
}

func (h handlers) status(ctx context.Context, in NodeInput) (modelnodes.View, error) {
	s, err := h.nodes()
	if err != nil {
		return modelnodes.View{}, err
	}
	v, err := s.Status(ctx, in.NodeID)
	return v, MapError(err)
}

// AcceptInput is the acceptance the user gave, as the status shows the versions.
type AcceptInput struct {
	NodeID        string `json:"node_id"`
	LicenseID     string `json:"license_id" jsonschema:"get_custom_node_status の license.id"`
	LicenseVer    string `json:"license_version" jsonschema:"get_custom_node_status の license.version"`
	NoticeVersion string `json:"notice_version" jsonschema:"get_custom_node_status の notice_version（security_notice の版）"`
}

// AcceptOutput is the node after the acceptance.
type AcceptOutput struct {
	NodeID   string `json:"node_id"`
	Accepted bool   `json:"accepted"`
}

func (h handlers) accept(ctx context.Context, in AcceptInput) (AcceptOutput, error) {
	s, err := h.nodes()
	if err != nil {
		return AcceptOutput{}, err
	}
	if err := s.Accept(in.NodeID, modelnodes.LicenseRef{ID: in.LicenseID, Version: in.LicenseVer, NoticeVersion: in.NoticeVersion}); err != nil {
		return AcceptOutput{}, MapError(err)
	}
	v, err := s.Status(ctx, in.NodeID)
	return AcceptOutput{NodeID: in.NodeID, Accepted: v.Accepted}, MapError(err)
}

// JobOutput is what the install answers with.
type JobOutput struct {
	mcpkit.JobRef
	Existing bool   `json:"existing,omitempty" jsonschema:"同じノードの導入がすでに動いていたので、新しいジョブは作らずそれを返した"`
	Target   string `json:"target" jsonschema:"node:<id>"`
}

func (h handlers) install(ctx context.Context, in NodeInput) (JobOutput, error) {
	s, err := h.nodes()
	if err != nil {
		return JobOutput{}, err
	}
	var existing bool
	target := modelnodes.TargetPrefix + in.NodeID
	ref, err := h.registrar.Jobs.Run(ctx, string(domain.JobModelInstall), target, func(ctx context.Context) (string, error) {
		jr, err := s.Install(ctx, in.NodeID, nil)
		existing = jr.Existing
		return jr.JobID, MapError(err)
	})
	return JobOutput{JobRef: ref, Existing: existing, Target: target}, err
}

// RemovedOutput says the node was removed.
type RemovedOutput struct {
	NodeID  string `json:"node_id"`
	Removed bool   `json:"removed"`
}

func (h handlers) remove(ctx context.Context, in NodeInput) (RemovedOutput, error) {
	s, err := h.nodes()
	if err != nil {
		return RemovedOutput{}, err
	}
	if err := s.Remove(ctx, in.NodeID); err != nil {
		return RemovedOutput{}, MapError(err)
	}
	return RemovedOutput{NodeID: in.NodeID, Removed: true}, nil
}

// JobProbe is the status of a job of a node, as the get_job Probe of modelmgr reports it (ok is false when the job is not a node's).
func JobProbe(ctx context.Context, svc *service.Services, jobID, target string) (p mcpkit.Progress, ok bool, err error) {
	if !strings.HasPrefix(target, modelnodes.TargetPrefix) || svc.ModelNodes == nil {
		return mcpkit.Progress{}, false, nil
	}
	st, err := svc.ModelNodes.JobStatus(ctx, jobID)
	if err != nil {
		return mcpkit.Progress{}, true, MapError(err)
	}
	p = mcpkit.Progress{Message: st.Progress.Message, Done: st.Progress.Percent, Total: 100, Error: st.Error}
	switch st.State {
	case domain.JobQueued:
		p.State = mcpkit.JobQueued
	case domain.JobRunning:
		p.State = mcpkit.JobRunning
	case domain.JobSucceeded:
		p.State = mcpkit.JobSucceeded
	default:
		p.State = mcpkit.JobFailed
	}
	p.Result = map[string]any{"target": st.Target, "phase": st.Progress.Phase, "done_bytes": st.Progress.DoneBytes, "total_bytes": st.Progress.TotalBytes}
	return p, true, nil
}

// Cancel stops a running install of a node by its job; it is the fallback of cancel_model_install for a job that is not the model manager's.
func Cancel(svc *service.Services, jobID string) error {
	if svc.ModelNodes == nil {
		return modelmanager.ErrNotFound
	}
	return svc.ModelNodes.CancelInstall(jobID)
}
