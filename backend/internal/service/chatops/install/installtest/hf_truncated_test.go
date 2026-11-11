package installtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
)

// A repository with more files than the list holds is cut: the only file in the list may not be the only file the repository has, and a file that is not in the list may be beyond it.
func TestHF_ACutListOfFilesIsToldOnTheCardOfTheFileTheListHolds(t *testing.T) {
	h := newHub()
	h.repo.Truncated = true

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "一覧は途中までです")
	assert.Contains(t, card.Text, "https://huggingface.co/owner/style")
}

func TestHF_ACutListIsToldOnTheCardThatOffersSeveralFiles(t *testing.T) {
	h := newHub()
	h.repo.Truncated = true
	h.repo.Files = append(h.repo.Files, install.HFFile{Path: "alt.safetensors", Name: "alt.safetensors", Size: gb, Installable: true, Folder: "loras"})

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	require.NoError(t, err)
	require.Len(t, card.Choices, 2)
	assert.Contains(t, card.Text, "一覧は途中までです")
}

func TestHF_AFileThatIsNotInACutListMayBeBeyondIt(t *testing.T) {
	h := newHub()
	h.repo.Truncated = true

	_, err := h.ask(t, install.HFArgs{Repo: "owner/style", Path: "nope.safetensors"})

	msg := refusal(t, err)
	assert.Contains(t, msg, "「nope.safetensors」というファイルはありません")
	assert.Contains(t, msg, "一覧は途中までしか読めていません")

	h = newHub()
	_, err = h.ask(t, install.HFArgs{Repo: "owner/style", Path: "nope.safetensors"})
	assert.NotContains(t, refusal(t, err), "途中まで", "a whole list says nothing about being cut")
}

func TestHF_ACutListWithoutInstallableFilesIsNotSaidToHaveNone(t *testing.T) {
	h := newHub()
	h.repo.Files = nil
	h.repo.Truncated = true

	_, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	assert.Contains(t, refusal(t, err), "一覧は途中までしか読めていません")
}

func TestHF_AWholeListHasNoNoteAboutBeingCut(t *testing.T) {
	h := newHub()

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	require.NoError(t, err)
	assert.NotContains(t, card.Text, "途中まで")
}
