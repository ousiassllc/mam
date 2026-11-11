package chatwire

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/nodegate"
)

// The chat operations of the installs (service/chatops/install, #1140): the same modelmanager.Service and localruntime.Service as the model manager screen and the MCP
// tools install_model, install_model_engine, install_hf_file and install_local_runtime. The handlers are given the services in the shapes of install.*Deps; the refusals of the
// services (no engine, a pinned file, no room, a consent missing) are told by the handlers with the sentences of the MCP tools (mmview.Refuse).
func init() {
	builders = append(builders, func(env Env, reg *chatops.Runtime) {
		var sources []install.JobSource
		if rts := env.LocalRuntimes; rts != nil {
			reg.Handle(install.OpInstallRuntime, install.NewRuntime(runtimeDeps(rts)))
			sources = append(sources, runtimeJob(rts)) // before the manager's: its JobStatus answers for the jobs of every kind of install
		}
		if m := env.ModelManager; m != nil {
			reg.Handle(install.OpInstallModel, install.NewModel(modelDeps(m)))
			reg.Handle(install.OpInstallEngine, install.NewEngine(engineDeps(m)))
			reg.Handle(install.OpInstallHFFile, install.NewHF(hfDeps(m)))
			sources = append(sources, managerJob(m))
		}
		if len(sources) > 0 {
			reg.Progress(install.Detail(sources...))
		}
	})
}

func licenseOf(l catalog.License) install.License {
	return install.License{
		ID: l.ID, Version: l.Version, Name: l.Name, URL: l.URL, Summary: l.Summary, Commercial: string(l.Commercial), Condition: l.CommercialCondition,
		Notice: l.Notice, ExcludedRegions: l.ExcludedRegions,
	}
}

func modelDeps(m *modelmanager.Service) install.ModelDeps {
	info := func(v modelmanager.ModelView) install.ModelInfo {
		out := install.ModelInfo{
			ID: v.ID, Name: v.Name, Kind: string(v.Kind), Adult: v.ContentRating == catalog.RatingAdult, License: licenseOf(v.License),
			TotalBytes: v.TotalSize, NeedBytes: v.NeedBytes, VRAMMinGB: v.VRAM.MinGB, ConsentRequired: v.ConsentRequired, Accepted: v.Accepted,
			RequiresHFToken: v.RequiresHFToken, AdultNotice: v.AdultNotice, Installed: v.State == modelmanager.ModelInstalled, InstallJob: v.InstallJob,
		}
		if !out.Installed {
			out.NodeReason = nodegate.Reason(m.Nodes, v.RequiresNodes)
		}
		return out
	}
	return install.ModelDeps{
		Catalog: func() []install.ModelInfo {
			views := m.Catalog()
			out := make([]install.ModelInfo, len(views))
			for i, v := range views {
				out[i] = info(v)
			}
			return out
		},
		Model: func(id string) (install.ModelInfo, error) {
			v, err := m.Model(id)
			return info(v), err
		},
		Environment: func(ctx context.Context) (install.Environment, error) { return environmentOf(ctx, m) },
		HasHFToken:  m.HasHuggingFaceToken,
		Install: func(ctx context.Context, id string, accept *install.LicenseRef) (install.Job, error) {
			var ref *modelmanager.LicenseRef
			if accept != nil {
				ref = &modelmanager.LicenseRef{ID: accept.ID, Version: accept.Version}
			}
			job, err := m.InstallModel(ctx, id, ref)
			return install.Job{ID: job.JobID, Existing: job.Existing}, err
		},
	}
}

func environmentOf(ctx context.Context, m *modelmanager.Service) (install.Environment, error) {
	st, err := m.Status(ctx)
	if err != nil {
		return install.Environment{}, err
	}
	e := st.Engine
	return install.Environment{Supported: e.Supported, Pinned: st.Pinned, Writable: e.Writable, EngineInstalled: e.Installed && !e.Broken, FreeBytes: e.FreeBytes}, nil
}

func engineDeps(m *modelmanager.Service) install.EngineDeps {
	return install.EngineDeps{
		Engine: func(ctx context.Context) (install.EngineInfo, error) {
			st, err := m.Status(ctx)
			if err != nil {
				return install.EngineInfo{}, err
			}
			e := st.Engine
			out := install.EngineInfo{
				Supported: e.Supported, Pinned: st.Pinned, Writable: e.Writable, Installed: e.Installed, Broken: e.Broken, UpdateAvailable: e.UpdateAvailable,
				Version: e.Version, PinnedVersion: e.PinnedVersion, FreeBytes: e.FreeBytes, InstallJob: e.InstallJob, LicenseName: e.License.Name, LicenseURL: e.License.URL,
			}
			for _, v := range e.Variants {
				out.Variants = append(out.Variants, install.Variant{ID: string(v.Variant), Label: v.Label, Size: v.Size, ExtractedSize: v.ExtractedSize, NeedBytes: v.NeedBytes})
			}
			// the install, and the update of an installed engine, choose the build by the GPU (Service.chooseVariant): the card must name the same one, so an update has a recommendation too
			if e.Supported && (!e.Installed || e.Broken || e.UpdateAvailable) {
				rec := m.DetectGPU(ctx).Recommendation
				out.RecommendedVariant, out.RecommendationUnavailable = string(rec.Variant), rec.Guidance
			}
			return out, nil
		},
		Install: func(ctx context.Context, variant string, cpu bool) (install.Job, error) {
			job, err := m.InstallEngine(ctx, modelmanager.EngineInstallInput{Variant: catalog.Variant(variant), CPU: cpu})
			return install.Job{ID: job.JobID, Existing: job.Existing}, err
		},
	}
}

func hfDeps(m *modelmanager.Service) install.HFDeps {
	return install.HFDeps{
		Environment: func(ctx context.Context) (install.Environment, error) { return environmentOf(ctx, m) },
		Repo: func(ctx context.Context, input string) (install.HFRepo, error) {
			v, err := m.HFRepo(ctx, input)
			if err != nil {
				return install.HFRepo{}, err
			}
			return hfRepoOf(v), nil
		},
		Install: func(ctx context.Context, repo, commit, path, terms string) (install.Job, error) {
			job, err := m.InstallHFFile(ctx, modelmanager.HFInstallInput{Repo: repo, Commit: commit, Path: path, Accept: terms != "", TermsVersion: terms})
			return install.Job{ID: job.JobID, Existing: job.Existing}, err
		},
	}
}

// hfRepoOf is the repository as get_hf_model_files reports it. The license is the one the model card declares, read as the screen and the MCP tool read it (modellicense.FromHF):
// a declaration alone is never 商用OK.
func hfRepoOf(v hfplan.RepoView) install.HFRepo {
	lic := modellicense.FromHF(v.Repo.License.ID, v.Repo.License.Name, v.Repo.License.Link)
	out := install.HFRepo{
		ID: v.Repo.ID, Commit: v.Repo.Commit, Page: v.Page, Gated: v.Repo.Gated, NFAA: v.Repo.NFAA, LicenseWarning: v.LicenseWarning, Truncated: v.Truncated,
		Selected: v.Selected, Terms: v.Terms, TermsVersion: v.TermsVersion, Consented: v.Consented, TokenConfigured: v.TokenConfigured,
		License: install.License{Name: lic.LicenseName, URL: lic.SourceURL, Commercial: string(lic.Commercial), Condition: lic.Condition, Notice: lic.Notice},
	}
	for _, f := range v.Files {
		out.Files = append(out.Files, install.HFFile{
			Path: f.Path, Name: f.Name, Size: f.Size, Installable: f.Installable, Reason: f.Reason, Folder: f.Folder, Security: f.Security, Installed: f.Installed, InstallJob: f.InstallJob,
		})
	}
	return out
}

func runtimeDeps(rts *localruntime.Service) install.RuntimeDeps {
	return install.RuntimeDeps{
		Runtimes: func(ctx context.Context) []install.RuntimeInfo {
			views := rts.List(ctx)
			out := make([]install.RuntimeInfo, len(views))
			for i, v := range views {
				out[i] = runtimeInfoOf(v)
			}
			return out
		},
		Install: func(ctx context.Context, id string) (install.Job, error) {
			job, err := rts.Install(ctx, id)
			return install.Job{ID: job.JobID, Existing: job.Existing}, err
		},
	}
}

func runtimeInfoOf(v localruntime.View) install.RuntimeInfo {
	out := install.RuntimeInfo{
		ID: v.ID, Name: v.Name, Description: v.Description, License: licenseOf(v.License), NotWritable: v.WritableError,
		Installed: v.Installed, Broken: v.Broken, Updates: v.UpdateAvailable, Version: v.Version, PinnedVersion: v.PinnedVersion, InstallJob: v.InstallJob,
		FreeBytes: v.FreeBytes, WeightsBytes: v.Weights.TotalBytes, WeightsUnclear: v.Weights.Unclear,
	}
	if !v.Support.Supported {
		out.Unsupported = v.Support.Message()
	}
	for _, a := range v.Variants {
		if a.Variant == v.Support.Variant {
			out.Variant = install.Variant{ID: string(a.Variant), Label: a.Label, Size: a.Size, ExtractedSize: a.ExtractedSize, NeedBytes: a.NeedBytes}
		}
	}
	return out
}

// managerJob reads the install jobs of the model manager (the engine, a model, a file of Hugging Face).
func managerJob(m *modelmanager.Service) install.JobSource {
	return func(ctx context.Context, id string) (install.JobProgress, bool) {
		st, err := m.JobStatus(ctx, id)
		if err != nil {
			return install.JobProgress{}, false
		}
		p := st.Progress
		return install.JobProgress{State: st.State, Percent: p.Percent, Phase: p.Phase, Message: p.Message, DoneBytes: p.DoneBytes, TotalBytes: p.TotalBytes, Error: st.Error}, true
	}
}

// runtimeJob reads the install jobs of the local runtimes.
func runtimeJob(rts *localruntime.Service) install.JobSource {
	return func(ctx context.Context, id string) (install.JobProgress, bool) {
		st, err := rts.JobStatus(ctx, id)
		if err != nil {
			return install.JobProgress{}, false
		}
		p := st.Progress
		return install.JobProgress{State: st.State, Percent: p.Percent, Phase: p.Phase, Message: p.Message, DoneBytes: p.DoneBytes, TotalBytes: p.TotalBytes, Error: st.Error}, true
	}
}
