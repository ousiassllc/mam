package imgprep

import "fmt"

// The preprocessors a ControlNet workflow names in its reference_preprocess (localcfg.Workflow.ReferencePreprocess).
const (
	// KindCanny makes the control image of a canny ControlNet from any picture (Canny).
	KindCanny = "canny"
)

// Kinds are the names Apply knows.
var Kinds = []string{KindCanny}

// Apply makes the control image of the preprocessor kind from the picture in data and returns it with its media type. A picture that already is
// the control image comes back unchanged (see Canny).
func Apply(kind string, data []byte, mime string) ([]byte, string, error) {
	switch kind {
	case KindCanny:
		out, made, err := Canny(data)
		if err != nil {
			return nil, "", err
		}
		if made {
			return out, "image/png", nil
		}
		return data, mime, nil
	}
	return nil, "", fmt.Errorf("imgprep: unknown preprocessor %q", kind)
}
