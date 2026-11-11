// Package loraimport draws the section "LoRA を追加" of the model manager (Issue #1119): choose or drop a LoRA file from the user's own PC, see what the
// file says of itself and complete it by hand, add it to the managed loras folder, and the list of what was added. The handler of the routes is
// web/handler/modelmgr/loraimport; every rule (the name, the format, the size, a name that is taken) is the service's, the screen only asks.
// It is a package of its own because the package of the screen is at its line budget (linterly).
package loraimport

import (
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Help is the "?" help of the section.
const Help = "Civitai などでダウンロードした LoRA（.safetensors）を、この PC の mam に取り込みます。ネットへは送りません。ファイルの形式と、LoRA のテンソルであることを確かめてから、管理しているフォルダ（loras）へコピーします。同じ名前のファイルは上書きしません。ファイルに書かれたベースモデル・トリガー語は読み取って入れますが、書かれていないものは「未設定」のままで、手で入力できます。ライセンスは任意で、入れなければ「不明」と表示します（商用OKにはしません）。取り込んだだけでは生成は始まりません。単発生成の「LoRA と強さ」で選びます。"

// The limits of the fields are the ones the service checks (a field longer than that is refused there).
const (
	NameMaxLen     = lorafile.MaxNameLen
	TriggersMaxLen = lorafile.MaxTriggerChars
	LicenseMaxLen  = lorafile.MaxLicenseRunes
)

// IDs of the elements of the section.
const (
	SectionID = "mm-lora"
	PanelID   = "mm-lora-panel"
	ListID    = "mm-lora-list"
	MsgID     = "mm-lora-msg"
)

// Names of the form fields (the handler reads the same names).
const (
	FieldFile       = "file"
	FieldToken      = "token"
	FieldName       = "name"
	FieldBase       = "base_models"
	FieldStrength   = "strength"
	FieldTriggers   = "trigger_words"
	FieldLicense    = "license_name"
	FieldCommercial = "commercial"
	FieldPath       = "path"
)

// URLs of the routes.
var (
	InspectURL = paths.ModelManagerPart("loras", "inspect")
	DiscardURL = paths.ModelManagerPart("loras", "discard")
	LoRAsURL   = paths.ModelManagerPart("loras")
)

// Family is a family of base model with its key and whether it is ticked.
type Family struct {
	Key, Label string
	Checked    bool
}

// Conflict is a name that is taken in the loras folder: the file there, whether it holds the same content, and a free name.
type Conflict struct {
	Name    string
	Same    bool
	Suggest string
}

// Staged is a file that was checked and waits for the user's confirmation, with what the form starts at (what the file says of itself, never more).
type Staged struct {
	Token, OriginalName string
	// Name is what the name field starts at: the name the file would get, or the free name that is suggested while it is taken.
	Name string
	// Renamed says characters of the file name were replaced; SavedAs is what it became.
	Renamed bool
	SavedAs string
	Size    string
	SHA256  string
	// Title, BaseRaw and Network are what the file says (shown as read: "" and none are not shown).
	Title   string
	BaseRaw []string
	Network string
	// Families are all the families, with the ones the file names ticked.
	Families []Family
	// FromFile says the base model was read from the file (the note under the checkboxes).
	FromFile bool
	// Triggers is the text the field starts at (the trigger words the file names); Candidates are the tags the training saw most often (a hint, never put in the field).
	Triggers   string
	Candidates []string
	// License is the license name the file names (the field starts at it; the answer to commercial use is never read from a file).
	License  string
	Conflict *Conflict
}

// Panel is the part above the list: the choose-a-file form, or the form of a file that waits.
type Panel struct {
	Notice, Error string
	Staged        *Staged
	MaxBytes      int64
	// MaxText is the limit as the screen writes it ("4.0 GiB").
	MaxText string
	// Unsupported is the reason files cannot be added here ("" when they can).
	Unsupported string
	// GentestHref is the generation test screen, where an added LoRA is chosen.
	GentestHref string
}

// Item is a LoRA the user added, as the list shows it. A field that is not set is "" / none: the screen says 未設定.
type Item struct {
	Path, Name, Size, SHA256 string
	// Present is false when the file is gone from the folder.
	Present  bool
	Families []Family
	Strength string
	Triggers string
	// License is the answer to commercial use as the user confirmed it (the unknown answer when they gave none).
	License     commercial.View
	LicenseName string
	CommercialV string
	AddedAt     string
}

// List is the LoRAs that were added.
type List struct {
	Items         []Item
	Notice, Error string
}
