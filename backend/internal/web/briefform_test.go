package web_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/briefform"
)

// #1114: the Brief form says which values are only a proposal; the user's own values carry no mark.
func TestBriefForm_MarksRecommendedAndInferredValues(t *testing.T) {
	b := domain.Brief{
		Subject: "猫", Duration: "15秒", AspectRatio: "9:16", Mood: "ゆるい",
		AdditionalInstruction: "制約: 成人",
		Sources: map[string]string{
			string(domain.BriefSubject):               domain.BriefSourceInferred,
			string(domain.BriefDuration):              domain.BriefSourceRecommended,
			string(domain.BriefAdditionalInstruction): domain.BriefSourceInferred,
		},
	}

	html := renderHTML(t, briefform.Form(briefform.Data{Project: domain.Project{ID: "p1"}, Brief: b}))

	assert.Equal(t, 2, strings.Count(html, `data-source="inferred_from_request"`), "Subject and Additional Instruction")
	assert.Equal(t, 1, strings.Count(html, `data-source="recommended"`), "Duration")
	assert.Contains(t, html, "推奨（仮）")
	assert.Contains(t, html, "保存すると、表示中の値")

	plain := renderHTML(t, briefform.Form(briefform.Data{Project: domain.Project{ID: "p1"}, Brief: domain.Brief{Subject: "猫"}}))
	assert.NotContains(t, plain, "data-source")
	assert.NotContains(t, plain, "保存すると、表示中の値")
}
