package audioinput

// The id of the list under the picker, the name of its select of the Project (a name of its own: the form posts every named field, and the picker of the
// pictures names its select "project") and the query of the address that answers with the row of a picked file, which says the role the row has.
const (
	PickerID     = "gt-audio-picker"
	ProjectField = "audio_project"
	RoleParam    = "role"
)

// Project is a Project whose audio assets (and images: organisms/gentest.ProjectChoice is this type) can be picked.
type Project struct {
	ID   string
	Name string
}

// Pick is one audio asset of a Project in the list under the picker: the value of the form field ref that names it, a label and the address of the file.
type Pick struct {
	Value string
	Label string
	URL   string
}

// Rows is where a picked audio file goes: the address that answers with its row, the list of the chosen inputs it is appended to, and the fields of the
// form that the answer needs (the media and the models, so that the notice follows the models that are ticked).
type Rows struct {
	URL     string
	Target  string
	Include string
}
