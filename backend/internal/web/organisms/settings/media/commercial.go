package media

import "github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"

// optionText is the text of a model option: only text fits in a <select>, so the commercial-use answer follows the name (#928).
func optionText(name string, answer commercial.View) string {
	return commercial.OptionLabel(name, answer)
}
