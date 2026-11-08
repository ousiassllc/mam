package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestBrief_MissingFollowsQuestionOrderAndSkipsDelegated(t *testing.T) {
	var b domain.Brief
	assert.Equal(t, domain.RequiredBriefFields(), b.Missing())

	b.Set(domain.BriefContentType, " 商品CM ")
	b.Delegate(domain.BriefTarget)
	assert.Equal(t, "商品CM", b.ContentType)
	assert.Equal(t, []domain.BriefField{
		domain.BriefObjective, domain.BriefSubject, domain.BriefDuration, domain.BriefAspectRatio, domain.BriefMood,
	}, b.Missing())
}

func TestBrief_SettingAValueAfterDelegatingTakesTheValue(t *testing.T) {
	var b domain.Brief
	b.Delegate(domain.BriefMood)
	b.Set(domain.BriefMood, "高級")
	assert.False(t, b.IsDelegated(domain.BriefMood))
	assert.Equal(t, "高級", b.Mood)
}

func TestBrief_AttachAssetsSortsImagesAndVideosIntoAssetsAndReferences(t *testing.T) {
	var b domain.Brief
	b.AttachAssets([]domain.Asset{
		{ID: "img", Type: domain.AssetImage},
		{ID: "vid", Type: domain.AssetVideo},
		{ID: "img", Type: domain.AssetImage},
	})
	assert.Equal(t, []string{"img"}, b.Assets)
	assert.Equal(t, []string{"vid"}, b.References)
}

func TestProjectName_PrefersSubjectThenFirstMessageAndTruncates(t *testing.T) {
	assert.Equal(t, "美容液", domain.ProjectName("美容液", "ignored"))
	assert.Equal(t, "こんにちは 世界", domain.ProjectName("", "  こんにちは\n世界 "))
	assert.Equal(t, domain.UntitledProject, domain.ProjectName(" ", ""))

	long := domain.ProjectName("", "あいうえおかきくけこ さしすせそたちつてと なにぬねのはひふへほ まみむめもやゆよらり")
	assert.Equal(t, 41, len([]rune(long)))
	assert.Equal(t, "…", string([]rune(long)[40:]))
}

// #1114: constraints the extractor read from the request are no topic, a line the user wrote may be one (#1108).
func TestBrief_InferredAdditionalInstructionNamesNoSubject(t *testing.T) {
	b := domain.Brief{AdditionalInstruction: "制約: 成人", Sources: map[string]string{string(domain.BriefAdditionalInstruction): domain.BriefSourceInferred}}
	assert.Contains(t, b.Missing(), domain.BriefSubject)
	b.Sources = nil
	assert.NotContains(t, b.Missing(), domain.BriefSubject)
	b.Duration, b.Sources = "15秒", map[string]string{string(domain.BriefDuration): domain.BriefSourceRecommended}
	b.Set(domain.BriefDuration, "30秒")
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefDuration), "a value the user sets is the user's")
}
