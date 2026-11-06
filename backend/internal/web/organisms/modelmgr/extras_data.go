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
