package traits

import (
	"slices"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Input is something a model takes as input. The names are the ones of the filter, the query string and the MCP tools.
type Input string

// The inputs. A model has every input that one of its workflows has.
const (
	T2I            Input = "t2i"               // text to image
	I2I            Input = "i2i"               // an image (reference) goes in and an image comes out
	Inpaint        Input = "inpaint"           // redraws the white of a mask
	Edit           Input = "edit"              // edits a picture the prompt describes the change of (the role "edit")
	T2V            Input = "t2v"               // text to video
	I2V            Input = "i2v"               // an image goes in and a video comes out (a first frame or reference images)
	FirstFrame     Input = "first_frame"       // the first frame of the clip
	LastFrame      Input = "last_frame"        // the last frame of the clip
	ReferenceWhole Input = "reference_whole"   // a reference picture as a whole (its look, people and objects)
	ReferenceFace  Input = "reference_face"    // a reference for the face (identity) only
	ReferenceStyle Input = "reference_style"   // a reference for the style and mood only
	ReferencePose  Input = "reference_pose"    // a reference for the pose only
	ReferenceCtl   Input = "reference_control" // a control image (a ControlNet edge map)
)

// order is the order the inputs are listed in.
var order = []Input{T2I, I2I, Inpaint, Edit, T2V, I2V, FirstFrame, LastFrame, ReferenceWhole, ReferenceFace, ReferenceStyle, ReferencePose, ReferenceCtl}

// All lists the inputs in the order the screens show them.
func All() []Input { return slices.Clone(order) }

// Valid reports whether i is an input.
func (i Input) Valid() bool { return slices.Contains(order, i) }

// Label is the short word of the badge and the filter.
func (i Input) Label() string {
	switch i {
	case T2I:
		return "T2I（文字から画像）"
	case I2I:
		return "I2I（画像から画像）"
	case Inpaint:
		return "インペイント"
	case Edit:
		return "画像編集"
	case T2V:
		return "T2V（文字から動画）"
	case I2V:
		return "I2V（画像から動画）"
	case FirstFrame:
		return "先頭フレーム"
	case LastFrame:
		return "最後のフレーム"
	case ReferenceWhole:
		return "参照画像（全体）"
	case ReferenceFace:
		return "参照画像（顔）"
	case ReferenceStyle:
		return "参照画像（スタイル）"
	case ReferencePose:
		return "参照画像（ポーズ）"
	case ReferenceCtl:
		return "制御画像"
	}
	return string(i)
}

// Facts are what one workflow (or one cloud model's description) says of its inputs: the single source the inputs of the catalog and of the
// generation test's model list are both derived from. FactsOf reads them from a workflow; a cloud model fills the fields from its capabilities.
type Facts struct {
	Video, Upscale bool
	// ReferenceMode is localcfg.ReferenceMode ("" for none or a plain img2img), MaxRefs the number of reference images, Roles the roles
	// (localcfg.Role*) a reference may have.
	ReferenceMode string
	MaxRefs       int
	Roles         []string
	// NeedsReference: the workflow cannot run without its reference (a ControlNet or an IP-Adapter workflow), so it is not text to image.
	NeedsReference        bool
	FirstFrame, LastFrame bool
	// FrameOptional: the first frame (or the reference images) may be left out and the model then makes the clip from the text alone: a cloud video
	// model. A ComfyUI workflow cannot (its frame is an input of the graph), so its text-to-video and image-to-video are two workflows.
	FrameOptional bool
	NativeAudio   bool
}

// FactsOf reads the facts of a workflow of a local model (a ComfyUI workflow of a catalog template).
func FactsOf(m localcfg.Model) Facts {
	f := Facts{Video: m.Media == localcfg.MediaVideo, Upscale: m.IsUpscale()}
	if m.Workflow == nil {
		return f
	}
	w := m.Workflow
	f.FirstFrame, f.LastFrame = w.Has(localcfg.ParamFirstFrame), w.Has(localcfg.ParamLastFrame)
	if m.Video != nil {
		f.NativeAudio = m.Video.NativeAudio
	}
	if f.Video {
		f.MaxRefs = w.Count(localcfg.ParamReferenceImage)
		return f
	}
	caps := m.ReferenceCaps()
	f.ReferenceMode, f.MaxRefs, f.Roles = string(caps.Mode), caps.Max, caps.Roles
	f.NeedsReference = caps.Mode == localcfg.ModeControlNet || caps.Mode == localcfg.ModeIPAdapter
	return f
}

// roleInput maps the role of a reference to its input.
var roleInput = map[string]Input{
	localcfg.RoleWhole: ReferenceWhole, localcfg.RoleFace: ReferenceFace, localcfg.RoleStyle: ReferenceStyle,
	localcfg.RoleEdit: Edit, localcfg.RolePose: ReferencePose,
}

// Inputs lists the inputs the facts give, in the fixed order. An upscale workflow gives none (it is a kind of its own).
func (f Facts) Inputs() []Input {
	if f.Upscale {
		return nil
	}
	set := map[Input]bool{}
	switch {
	case f.Video:
		set[T2V] = f.FrameOptional || (!f.FirstFrame && f.MaxRefs == 0)
		set[I2V] = f.FirstFrame || f.MaxRefs > 0
		set[FirstFrame], set[LastFrame] = f.FirstFrame, f.LastFrame
		set[ReferenceWhole] = f.MaxRefs > 0
	default:
		set[T2I] = !f.NeedsReference
		switch localcfg.ReferenceMode(f.ReferenceMode) {
		case localcfg.ModeInpaint:
			set[Inpaint], set[I2I] = true, true
		case localcfg.ModeControlNet:
			set[ReferenceCtl] = true
		case localcfg.ModeIPAdapter:
			set[ReferenceWhole] = true
		default:
			if f.MaxRefs > 0 {
				set[I2I] = true
				for _, r := range f.Roles {
					set[roleInput[r]] = true
				}
				if len(f.Roles) == 0 {
					set[ReferenceWhole] = true
				}
			}
		}
	}
	var out []Input
	for _, i := range order {
		if set[i] {
			out = append(out, i)
		}
	}
	return out
}

// Derived is what the workflows of a model say, joined: the inputs (the union over the workflows) and whether any of them has sound.
type Derived struct {
	Inputs []Input
	Audio  bool
}

// Derive joins the facts of the workflows of one model.
func Derive(facts []Facts) Derived {
	var d Derived
	have := map[Input]bool{}
	for _, f := range facts {
		d.Audio = d.Audio || f.NativeAudio
		for _, i := range f.Inputs() {
			have[i] = true
		}
	}
	for _, i := range order {
		if have[i] {
			d.Inputs = append(d.Inputs, i)
		}
	}
	return d
}

// Has reports whether the model takes the input.
func (d Derived) Has(i Input) bool { return slices.Contains(d.Inputs, i) }

// templateFacts is the facts of each embedded workflow template, read once: a template is a JSON file of the binary that does not change, and
// reading one decodes its whole graph, which the model manager's list (rendered every two seconds) must not do for every model each time.
var templateFacts sync.Map // template ID -> Facts

// TemplateFacts are the facts of the embedded workflow template with the ID; false when there is none.
func TemplateFacts(id string) (Facts, bool) {
	if f, ok := templateFacts.Load(id); ok {
		return f.(Facts), true
	}
	tpl, ok := localcfg.TemplateByID(id)
	if !ok {
		return Facts{}, false
	}
	f := FactsOf(tpl.Model)
	templateFacts.Store(id, f)
	return f, true
}
