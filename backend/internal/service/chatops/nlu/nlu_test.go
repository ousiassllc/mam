package nlu_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
)

func TestNorm(t *testing.T) {
	assert.Equal(t, "flux1schnell", nlu.Norm("FLUX.1 schnell"))
	assert.Equal(t, nlu.Norm("flux1-schnell"), nlu.Norm("FLUX.1 schnell"))
	assert.Empty(t, nlu.Norm(" -. "))
}

func TestFew(t *testing.T) {
	assert.Equal(t, "a、b", nlu.Few([]string{"a", "b"}, 2))
	assert.Equal(t, "a、ほか 2 件", nlu.Few([]string{"a", "b", "c"}, 1))
}

func TestPerson(t *testing.T) {
	assert.Equal(t, "を入れて", nlu.Person.ReplaceAllString("プロのモデルを入れて", ""))
	assert.Equal(t, "を入れて", nlu.Person.ReplaceAllString("モデルさんを入れて", ""))
	assert.Equal(t, "モデルを入れて", nlu.Person.ReplaceAllString("モデルを入れて", ""))
}

func TestIsInquiry(t *testing.T) {
	assert.True(t, nlu.IsInquiry("今の画像モデルは何ですか？"))
	assert.True(t, nlu.IsInquiry("Director の LLM を教えて"))
	assert.False(t, nlu.IsInquiry("Director の LLM を Claude にしてくれる？"))
	assert.False(t, nlu.IsInquiry("Director の LLM を Claude にして"))
}

func TestRoleOf(t *testing.T) {
	assert.Equal(t, nlu.RoleFallback, nlu.RoleOf("代替"))
	assert.Equal(t, nlu.RoleFallback, nlu.RoleOf("Fallback"))
	assert.Equal(t, nlu.RolePrimary, nlu.RoleOf(""))
	assert.Equal(t, nlu.RolePrimary, nlu.RoleOf("プライマリ"))
}

func TestAgentIn_TheLongestNameWins(t *testing.T) {
	assert.Equal(t, "art_director", nlu.AgentIn("Art Director の LLM を変えて"))
	assert.Equal(t, "producer", nlu.AgentIn("Producer の代替"))
	assert.Empty(t, nlu.AgentIn("カメラを固定して"))
}
