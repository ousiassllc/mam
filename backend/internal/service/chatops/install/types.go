package install

import "context"

// The handlers depend on what the service layer says of a model, an engine, a repository or a runtime, in these shapes, not on the services themselves: service/chatwire
// adapts modelmanager.Service and localruntime.Service to them (chatwire/install.go) and the tests fake them. The package still names the error values of the services
// (modelmanager.ErrPinned ...) and takes the sentences of the MCP tools (mmview.Refuse, mmview.RefuseRuntime), so that a refusal (no engine, a pinned settings file, no
// room) is told in the very words the tools use.

// License is what the card says of a license.
type License struct {
	ID, Version, Name, URL, Summary string
	// Commercial is "yes", "conditional", "no" or "unknown" (modellicense.Commercial); Condition says on what a conditional model may be used commercially.
	Commercial, Condition string
	// Notice is what to watch for; ExcludedRegions are the regions the license does not apply in.
	Notice          string
	ExcludedRegions []string
}

// LicenseRef is the license a user accepted: the ID and version the card showed.
type LicenseRef struct{ ID, Version string }

// Job is the install the service queued.
type Job struct {
	ID string
	// Existing: the same install was already running, so no new job was made.
	Existing bool
}

// Environment is where an install lands: the bundled ComfyUI engine and its directory.
type Environment struct {
	// Supported: this OS has the bundled engine. Pinned: the model settings file was fixed by a start option. Writable: the install directory can be written to.
	Supported, Pinned, Writable bool
	// EngineInstalled is false when the engine is not installed or its files are gone.
	EngineInstalled bool
	// FreeBytes is the free space of the install directory (0: not known).
	FreeBytes uint64
}

// ModelInfo is a model of the catalog of the model manager.
type ModelInfo struct {
	ID, Name string
	// Kind is "image", "video" ... as the catalog says.
	Kind    string
	Adult   bool
	License License
	// TotalBytes is what the model's files weigh; NeedBytes is what is still to fetch (a file that is there or half fetched does not count).
	TotalBytes, NeedBytes int64
	// VRAMMinGB is the video memory the model needs at least (0: not stated).
	VRAMMinGB int
	// ConsentRequired: the license has to be accepted before the install; Accepted: it was, in this version.
	ConsentRequired, Accepted bool
	RequiresHFToken           bool
	// AdultNotice is the notice of an adult model.
	AdultNotice string
	// NodeReason says why the model cannot be installed for lack of custom nodes ("" when it can).
	NodeReason string
	// Installed: the model is in place. InstallJob is the install that is queued or running ("" for none).
	Installed  bool
	InstallJob string
}

// ModelDeps is what OpInstallModel needs of the model manager. Catalog lists the models that may be offered (an adult model only while the age gate is open); Model reads one
// by its id and refuses as the MCP tool get_catalog_model does (an unknown id, an adult model before the age confirmation). Install is the same call as install_model;
// it returns the refusals of the service as *chatops.Refusal.
type ModelDeps struct {
	Catalog     func() []ModelInfo
	Model       func(id string) (ModelInfo, error)
	Environment func(ctx context.Context) (Environment, error)
	// HasHFToken says whether a Hugging Face token is stored (a model behind a login needs one).
	HasHFToken func() (bool, error)
	Install    func(ctx context.Context, id string, accept *LicenseRef) (Job, error)
}

// Variant is a build of the engine or of a runtime with what it needs.
type Variant struct {
	ID, Label string
	// Size is the download; ExtractedSize what it takes once unpacked; NeedBytes the free space the install needs (what is not downloaded yet and the unpacked files).
	Size, ExtractedSize, NeedBytes int64
}

// EngineInfo is the bundled ComfyUI engine.
type EngineInfo struct {
	Supported, Pinned, Writable bool
	Installed, Broken           bool
	// UpdateAvailable: the installed version is not the pinned one, so installing again updates it.
	UpdateAvailable           bool
	Version, PinnedVersion    string
	FreeBytes                 uint64
	InstallJob                string
	LicenseName, LicenseURL   string
	Variants                  []Variant
	RecommendedVariant        string // the build of this machine's GPU ("" when no supported GPU was found)
	RecommendationUnavailable string // what to do instead when RecommendedVariant is ""
}

// EngineDeps is what OpInstallEngine needs. Install is install_model_engine; variant "" takes the build of the GPU.
type EngineDeps struct {
	Engine  func(ctx context.Context) (EngineInfo, error)
	Install func(ctx context.Context, variant string, cpu bool) (Job, error)
}

// HFFile is a file of a Hugging Face repository.
type HFFile struct {
	Path, Name  string
	Size        int64
	Installable bool
	// Reason says why the file cannot be installed.
	Reason string
	// Folder is where the file would go ("" when mam cannot tell).
	Folder     string
	Security   string
	Installed  bool
	InstallJob string
}

// HFRepo is a repository pinned to the commit the Hub gave (hfplan.RepoView).
type HFRepo struct {
	ID, Commit, Page string
	Gated, NFAA      bool
	License          License
	// LicenseWarning says the model card gives no usable license.
	LicenseWarning string
	Files          []HFFile
	Truncated      bool
	// Selected is the file a blob or resolve URL pointed at ("" for none).
	Selected string
	// Terms and TermsVersion are what the user confirms; Consented: they were confirmed for this repository and commit.
	Terms, TermsVersion string
	Consented           bool
	// TokenConfigured: a Hugging Face token is stored (a gated repository needs one).
	TokenConfigured bool
}

// HFDeps is what OpInstallHFFile needs. Repo is get_hf_model_files: it resolves the input (owner/repo or a URL), pins the commit and refuses what the tool refuses (an adult repository
// before the age confirmation, a gated one without a token, a repository that is gone). Install is install_hf_file; acceptedTerms is "" when the terms were not confirmed
// now.
type HFDeps struct {
	Repo        func(ctx context.Context, input string) (HFRepo, error)
	Environment func(ctx context.Context) (Environment, error)
	Install     func(ctx context.Context, repo, commit, path, acceptedTerms string) (Job, error)
}

// RuntimeInfo is a local voice / lip-sync runtime.
type RuntimeInfo struct {
	ID, Name, Description string
	License               License
	// Unsupported is the reason (and what to use instead) when this machine cannot run it ("" when it can).
	Unsupported string
	// NotWritable says why the install directory cannot be written to ("" when it can).
	NotWritable                string
	Installed, Broken, Updates bool
	Version, PinnedVersion     string
	InstallJob                 string
	FreeBytes                  uint64
	// Variant is the build for this machine; WeightsBytes is what the weights weigh and WeightsUnclear how many of them have a commercial use that is not "yes".
	Variant        Variant
	WeightsBytes   int64
	WeightsUnclear int
}

// RuntimeDeps is what OpInstallRuntime needs. Runtimes lists the catalog; Install is install_local_runtime.
type RuntimeDeps struct {
	Runtimes func(ctx context.Context) []RuntimeInfo
	Install  func(ctx context.Context, id string) (Job, error)
}
