package modellicense

import (
	"bytes"
	_ "embed" // data/cloud.json
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

//go:embed data/cloud.json
var cloudJSON []byte

var (
	cloudOnce sync.Once
	cloud     map[string]Info
)

// loadCloud reads data/cloud.json, refusing unknown fields. It panics when the embedded data is invalid: a build problem
// that the package's tests catch.
func loadCloud() map[string]Info {
	cloudOnce.Do(func() {
		dec := json.NewDecoder(bytes.NewReader(cloudJSON))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cloud); err != nil {
			panic(fmt.Sprintf("modellicense: data/cloud.json: %v", err))
		}
	})
	return cloud
}

// Cloud returns the license of a built-in cloud model (a model that mam reaches through a vendor's API or a subscription), by
// the identifier the generation test uses ("nano-banana", "kling-3.0"). A model that is added to the adapter needs an entry in
// data/cloud.json: the test of the model list fails until it has one.
func Cloud(id string) (Info, bool) {
	i, ok := loadCloud()[id]
	return i, ok
}

// CloudIDs lists the models that have an entry in data/cloud.json, in alphabetical order.
func CloudIDs() []string {
	ids := make([]string, 0, len(loadCloud()))
	for id := range loadCloud() {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
