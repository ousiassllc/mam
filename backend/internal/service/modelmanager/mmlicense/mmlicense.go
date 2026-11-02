// Package mmlicense is what the model manager says of commercial use (#928): the license of a model of the bundled endpoint, and the
// setting "商用OKのモデルだけを使う". It is a package of its own because the directory of the model manager is full (linterly); the
// Service embeds a Book, so the methods are the Service's.
package mmlicense

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// Record is the part of the model manager's state.json a Book reads and writes.
type Record interface {
	Get() store.State
	Update(func(*store.State)) error
}

// Book answers for the models of one catalog and one record.
type Book struct {
	cat      *catalog.Catalog
	state    Record
	endpoint string
}

// New returns the Book of the catalog and the record; endpoint is the name of the bundled endpoint (the models that mam installs).
func New(cat *catalog.Catalog, state Record, endpoint string) *Book {
	return &Book{cat: cat, state: state, endpoint: endpoint}
}

// LicenseOf is the license of a model of the bundled endpoint (comfyui:<endpoint>/<name>): the license of the catalog model whose
// workflow it is, or of the file imported from Hugging Face that made it (the license tag of the card, read when it was imported).
// It is false for any other model: that is a model of another endpoint, which mam did not install. Pass it to
// modellicense.SetResolver so that every screen and tool shows the answer.
func (b *Book) LicenseOf(id string) (modellicense.Info, bool) {
	kind, endpoint, name, ok := localcfg.ParseID(id)
	if !ok || kind != localcfg.KindComfyUI || endpoint != b.endpoint {
		return modellicense.Info{}, false
	}
	for _, m := range b.cat.Models {
		for _, w := range m.Workflows {
			if w.Name == name {
				return m.License.Info(), true
			}
		}
	}
	for _, um := range b.state.Get().UserModels {
		for _, w := range um.Workflows {
			if w == name {
				return modellicense.FromHF(um.License.ID, um.License.Name, um.License.Link), true
			}
		}
	}
	return modellicense.Info{}, false
}

// CommercialOnly is the setting "商用OKのモデルだけを使う": the filter on commercial use that the screens and the MCP lists start with is
// "商用OKのみ". It narrows the lists the user sees by default; it does not stop a generation with another model.
func (b *Book) CommercialOnly() bool { return b.state.Get().CommercialOnly }

// SetCommercialOnly turns the setting on or off.
func (b *Book) SetCommercialOnly(on bool) error {
	return b.state.Update(func(st *store.State) { st.CommercialOnly = on })
}

// DefaultFilter is the filter the lists start with: "商用OKのみ" while the setting is on, all models otherwise.
func (b *Book) DefaultFilter() modellicense.Filter {
	if b.CommercialOnly() {
		return modellicense.FilterOK
	}
	return modellicense.FilterAll
}
