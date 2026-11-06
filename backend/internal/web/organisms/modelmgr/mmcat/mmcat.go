// Package mmcat is the one list of the categories of the model manager (spec/components/model-manager-screen.md, "分類のサブページ（#1101）"):
// the routes (GET /model-manager/<slug>), the second level of the side menu, the cards of the overview and the tests are all made from All. A
// new category is one more row there (and its sections in the body of organisms/modelmgr). The package depends on atoms and paths only, so that
// the side menu and the pages can both import it.
package mmcat

import (
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The slugs of the categories (paths.MM*: the places that only link to a category use those).
const (
	ComfyUI       = paths.MMComfyUI
	ImageVideo    = paths.MMImageVideo
	CustomNodes   = paths.MMCustomNodes
	LocalRuntimes = paths.MMLocalRuntimes
	FaceCheck     = paths.MMFaceCheck
	HFExternal    = paths.MMHFExternal
)

// Category is a sub-page of the model manager: its name (the h1, the menu and the card), a one-line description and the sections it holds.
type Category struct {
	Slug        string
	Name        string
	Description string
	Icon        atoms.IconName
	// Sections are the ids of the sections of the page that are always there (an element of the page, whatever is installed). No other category has any of them.
	Sections []string
}

// Href is the page of the category.
func (c Category) Href() string { return paths.ModelManagerPart(c.Slug) }

// All is every category, in the order of the menu and of the overview.
func All() []Category {
	return []Category{
		{
			Slug: ComfyUI, Name: "ComfyUI エンジン", Icon: atoms.IconWorkflow,
			Description: "画像・動画のモデルを動かす ComfyUI を導入して起動します。すでに動いている ComfyUI も検出できます。",
			Sections:    []string{"mm-engine", "mm-detect"},
		},
		{
			Slug: ImageVideo, Name: "画像・動画モデル", Icon: atoms.IconImage,
			Description: "画像・動画のモデルを導入・削除します。成人向けモデルの表示と、商用OKのモデルだけを使う設定もここです。",
			Sections:    []string{"mm-adult", "mm-commercial", "mm-catalog", "mm-dialog"},
		},
		{
			Slug: CustomNodes, Name: "追加ノード", Icon: atoms.IconNetwork,
			Description: "ComfyUI に機能を足すカスタムノードを、mam が確認して固定したものだけ導入します。",
			Sections:    []string{"mm-nodes"},
		},
		{
			Slug: LocalRuntimes, Name: "ローカル音声・口パク", Icon: atoms.IconVoice,
			Description: "声と口パクをこの PC で作る実行環境を導入して、起動・停止します。",
			Sections:    []string{"mm-runtimes"},
		},
		{
			Slug: FaceCheck, Name: "顔チェック用モデル", Icon: atoms.IconLipSync,
			Description: "生成した画像の顔が、参照の顔にどれだけ似ているかを測る小さなモデルを導入します。",
			Sections:    []string{"mm-face-models"},
		},
		{
			Slug: HFExternal, Name: "Hugging Face・外部の ComfyUI", Icon: atoms.IconPackage,
			Description: "Hugging Face のモデルファイルを探して導入します。トークンと、別のマシンにある ComfyUI の確認もここです。",
			Sections:    []string{"mm-hfi", "mm-hf", "mm-external"},
		},
	}
}

// Find is the category with the slug.
func Find(slug string) (Category, bool) {
	for _, c := range All() {
		if c.Slug == slug {
			return c, true
		}
	}
	return Category{}, false
}
