package modelsettings_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

type noCredentials struct{}

func (noCredentials) ImageCredentials(context.Context) (chatgpt.Credentials, error) {
	return chatgpt.Credentials{}, chatgpt.ErrImageDisabled
}

// The subscription's image model can be chosen for a purpose while its switch is on, and only then.
func TestSave_TheSubscriptionImageModelIsOfferedWhileItIsOn(t *testing.T) {
	on := false
	opts := localbackends.Options{
		ChatGPTImage: chatgptimage.New(noCredentials{}, chatgptimage.Options{}), ChatGPTImageEnabled: func() bool { return on },
	}
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	in := defaults()
	in.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: chatgptid.Model},
		Fallback: modeladapter.DefaultConfig().Images[modeladapter.PurposeNewReference].Primary,
	}

	svc, _ := newService(t)
	opts.Publish(modeladapter.DefaultConfig())
	assert.Error(t, svc.Save(in), "off: not offered")

	on = true
	opts.Publish(modeladapter.DefaultConfig())
	require.NoError(t, svc.Save(in))
	view, err := svc.View()
	require.NoError(t, err)
	assert.Equal(t, chatgptid.Model, view.Images[modeladapter.PurposeNewReference].Primary.Model)
}
