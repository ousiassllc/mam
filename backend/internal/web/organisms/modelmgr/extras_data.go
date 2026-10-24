package modelmgr

// HFToken is the optional Hugging Face token: whether there is one (and its end), never its value.
type HFToken struct {
	Message
	Configured bool
	LastChars  string
	// Unavailable: the key store cannot be read.
	Unavailable bool
}

// ExternalModel is a catalog model against an external ComfyUI.
type ExternalModel struct {
	Name     string
	Complete bool
	Present  []string
	Missing  []string
}

// ExternalFolder is a model folder of an external ComfyUI with its files.
type ExternalFolder struct {
	Name  string
	Files []string
}

// ExternalResult is what an external ComfyUI has.
type ExternalResult struct {
	Endpoint string
	Version  string
	Folders  []ExternalFolder
	Models   []ExternalModel
}

// External is the view of an external ComfyUI: the endpoints to choose from and, once asked, the result.
type External struct {
	Message
	Endpoints []string
	Selected  string
	Result    *ExternalResult
}

// TrialModel is a choice of the model select.
type TrialModel struct {
	ID       string
	Label    string
	Selected bool
}

// TrialForm is the form that starts a trial generation, for the model that is chosen.
type TrialForm struct {
	Message
	Models []TrialModel
	// Media is image or video ("" while no model is chosen).
	Media            string
	AcceptsReference bool
	Resolutions      []string
	Durations        []string
	Prompt           string
	Negative         string
	Aspect           string
	Resolution       string
	MaxPrompt        int
	Duration         string
}

// Trial is a trial generation of the list.
type Trial struct {
	ID         string
	ModelID    string
	Video      bool
	Prompt     string
	Detail     string
	Reference  bool
	Status     string
	Error      string
	CreatedAt  string
	Size       string
	FileURL    string
	Progress   *Progress
	Deletable  bool
	StatusText string
}

// Trials is the list of trials.
type Trials struct {
	Message
	Items []Trial
	// Jobs are the trial jobs that have no record yet (queued, or just started): the list asks for them until they have.
	Jobs []string
}

// Polling reports whether a trial is running.
func (t Trials) Polling() bool {
	if len(t.Jobs) > 0 {
		return true
	}
	for _, it := range t.Items {
		if it.Status == "running" {
			return true
		}
	}
	return false
}

// TrialPanel is the trial form and the results.
type TrialPanel struct {
	Form   TrialForm
	Trials Trials
	// OOB draws the section out of band (a model was installed or deleted: the choices changed).
	OOB bool
	// Unavailable says why no trial can start ("" when one can): no model is installed or registered.
	Unavailable string
}
