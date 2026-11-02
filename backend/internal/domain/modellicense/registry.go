package modellicense

import (
	"strings"
	"sync/atomic"
)

// Resolver answers for a model that is not a built-in cloud model: the models the model manager made (a model of its catalog, a file
// imported from Hugging Face). It returns false for a model it does not know.
type Resolver func(id string) (Info, bool)

var resolver atomic.Pointer[Resolver]

// SetResolver installs the resolver for the models of the model manager (nil removes it). The set is process-wide, like
// modelreg: every screen and tool that lists a model asks Of without a handle on the model manager.
func SetResolver(r Resolver) {
	if r == nil {
		resolver.Store(nil)
		return
	}
	resolver.Store(&r)
}

// NotChecked is the notice of a model nobody checked: a model the user defined on an endpoint, a saved workflow, a model whose
// card says nothing.
const NotChecked = "このモデルのライセンスを mam は確認できていません。商用で使う前に、モデルの配布元でライセンスを確かめてください。"

// Of is the license of the model with the identifier the generation test uses: a built-in cloud model from data/cloud.json, a model
// of the model manager from the resolver, and Unknown (with NotChecked) for any other.
func Of(id string) Info {
	if i, ok := Cloud(id); ok {
		return i
	}
	if r := resolver.Load(); r != nil {
		if i, ok := (*r)(id); ok {
			return i
		}
	}
	return UnknownInfo(NotChecked)
}

// ReasonOf is the sentence of a tooltip: the license name, and the condition or the notice when there is one.
func (i Info) ReasonOf() string {
	parts := []string{i.LicenseName}
	switch {
	case i.Condition != "":
		parts = append(parts, i.Condition)
	case i.Commercial != Yes && i.Notice != "":
		parts = append(parts, i.Notice)
	}
	return strings.Join(parts, " ／ ")
}

var defaultFilter atomic.Pointer[func() Filter]

// SetDefaultFilter installs what the lists of models start at: the setting "商用OKのモデルだけを使う" ("商用OKのみ" while it is on). Process-wide like
// SetResolver, so that every screen and tool that lists models asks DefaultFilter without a handle on the model manager (nil removes it).
func SetDefaultFilter(f func() Filter) {
	if f == nil {
		defaultFilter.Store(nil)
		return
	}
	defaultFilter.Store(&f)
}

// DefaultFilter is the filter the lists of models start at: FilterAll unless the setting says otherwise.
func DefaultFilter() Filter {
	if f := defaultFilter.Load(); f != nil {
		return (*f)()
	}
	return FilterAll
}
