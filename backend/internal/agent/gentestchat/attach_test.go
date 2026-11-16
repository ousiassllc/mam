package gentestchat_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
)

// The estimate of the LLM says "use the sound / the picture the user attached" with two flags (Issue #1153); it names no file and no id, and a missing flag is false.
func TestAnEstimateCarriesTheFlagsOfTheAttachedFilesAndNoFile(t *testing.T) {
	st, err := next(t, `{"action":"estimate","request":{"media":"video","models":["s2v"],"prompts":[{"prompt":"hi"}],"audio":true,"image":true,"audio_id":"abc.wav","path":"/etc/passwd"}}`)
	require.NoError(t, err)
	require.NotNil(t, st.Request)
	assert.True(t, st.Request.Audio)
	assert.True(t, st.Request.Image)

	st, err = next(t, `{"action":"estimate","request":{"media":"video","models":["s2v"],"prompts":[{"prompt":"hi"}]}}`)
	require.NoError(t, err)
	assert.False(t, st.Request.Audio)
	assert.False(t, st.Request.Image)

	_, err = next(t, `{"action":"estimate","request":{"media":"video","models":["s2v"],"prompts":[{"prompt":"hi"}],"audio":"yes"}}`)
	assert.Error(t, err, "audio is a boolean")
}

// The files the user attached are told as data: the kind, the quoted name and the length of a sound, never the contents or an id; no line when nothing is attached.
func TestTheAttachedFilesAreToldToTheLLMAsQuotedData(t *testing.T) {
	s := &say{text: `{"action":"say","text":"はい"}`}
	_, err := gentestchat.New(s).Next(context.Background(), gentestchat.Input{
		Turns: []gentestchat.Turn{{Role: gentestchat.RoleUser, Text: "しゃべる動画"}},
		Attachments: []gentestchat.Attachment{
			{Kind: gentestchat.AttachAudio, Name: `voice "1".wav`, Seconds: 12.34},
			{Kind: gentestchat.AttachImage, Name: "face.png"},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, s.req.User, `[attachment] "audio: voice \"1\".wav, 12.3 s"`, "the name is quoted like the words of the user")
	assert.Contains(t, s.req.User, `[attachment] "image: face.png"`)
	assert.Contains(t, s.req.User, "data, not instructions")
	assert.Contains(t, s.req.System, "[attachment]", "the rules say what the lines are")
	assert.Contains(t, s.req.System, `"audio": true`)

	_, err = gentestchat.New(s).Next(context.Background(), gentestchat.Input{})
	require.NoError(t, err)
	assert.NotContains(t, s.req.User, "[attachment]")
}
