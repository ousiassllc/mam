package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestParseTags_SplitsDeduplicatesAndChecksLimits(t *testing.T) {
	tags, err := domain.ParseTags(" Hero,hero 主役、close-up　 \n night,,")
	require.NoError(t, err)
	assert.Equal(t, []string{"Hero", "主役", "close-up", "night"}, tags)

	tags, err = domain.ParseTags("")
	require.NoError(t, err)
	assert.NotNil(t, tags)
	assert.Empty(t, tags)

	_, err = domain.ParseTags(strings.Repeat("a", domain.MaxReferenceTagRunes+1))
	assert.Error(t, err)
	_, err = domain.ParseTags(strings.Repeat("あ", domain.MaxReferenceTagRunes))
	assert.NoError(t, err, "the limit counts characters, not bytes")
	_, err = domain.ParseTags("1 2 3 4 5 6 7 8 9 10 11")
	assert.Error(t, err)
	_, err = domain.ParseTags("1 2 3 4 5 6 7 8 9 10")
	assert.NoError(t, err)
}

func TestLibraryKinds_ExcludeTheKeyframe(t *testing.T) {
	assert.False(t, domain.ValidLibraryKind(domain.RefKeyframe))
	assert.False(t, domain.ValidLibraryKind("bogus"))
	for _, k := range domain.LibraryKinds() {
		assert.True(t, domain.ValidLibraryKind(k))
	}
}

func TestLockableFields_CoverEditableFieldsAndTheReferenceAttributes(t *testing.T) {
	assert.Contains(t, domain.LockableFields(domain.TargetCharacter), "wardrobe")
	assert.Contains(t, domain.LockableFields(domain.TargetCharacter), "identity")
	assert.Contains(t, domain.LockableFields(domain.TargetProduct), "geometry")
	assert.Contains(t, domain.LockableFields(domain.TargetProduct), "orientation")
	assert.Contains(t, domain.LockableFields(domain.TargetLocation), "layout_reference")
	assert.Contains(t, domain.LockableFields(domain.TargetLocation), "lighting_state")
	assert.Contains(t, domain.LockableFields(domain.TargetStory), "scene")
	assert.False(t, domain.IsLockable(domain.TargetCharacter, "layout_reference"))
	assert.False(t, domain.IsLockable(domain.TargetLocation, "name"))
	assert.True(t, domain.IsExtractable(domain.TargetLocation, "weather"))
	assert.False(t, domain.IsExtractable(domain.TargetLocation, "layout_reference"), "the layout image is set by the user only")
}

func TestReferenceSlots_EachSlotIsLockedByItsOwnAttribute(t *testing.T) {
	s, ok := domain.FindReferenceSlot(domain.TargetCharacter, "face")
	require.True(t, ok)
	assert.Equal(t, "identity", s.Lock)
	assert.Equal(t, domain.RefCharacter, s.Kind)
	_, ok = domain.FindReferenceSlot(domain.TargetCharacter, "layout")
	assert.False(t, ok)
	for _, slot := range domain.ReferenceSlots() {
		assert.True(t, domain.IsLockable(slot.Target, slot.Lock), slot.Name)
	}
}
