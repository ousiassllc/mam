package artdirector_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/artdirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type fakeLLM struct {
	answer string
	agent  llm.Agent
	user   string
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.agent, f.user = agent, req.User
	return llm.Response{Text: f.answer, Provider: llm.ProviderGoogle, Model: "fake"}, nil
}

func design(answer string, in artdirector.Input) ([]artdirector.Plan, error) {
	return artdirector.New(&fakeLLM{answer: answer}).Design(context.Background(), in)
}

func TestDesign_OrdersReferencesByKindAndKeepsUploadFlags(t *testing.T) {
	f := &fakeLLM{answer: `{"references":[
		{"kind":"keyframe","prompt":"The first frame"},
		{"kind":"style","prompt":"Cold palette"},
		{"kind":"product","name":"Serum","prompt":"A serum bottle","use_uploaded":true}]}`}
	plans, err := artdirector.New(f).Design(context.Background(), artdirector.Input{
		Brief: domain.Brief{Subject: "美容液"}, Shot: domain.Shot{Objective: "商品を見せる", Duration: 3}, UploadedImages: 2,
	})
	require.NoError(t, err)

	assert.Equal(t, llm.AgentArtDirector, f.agent)
	kinds := []domain.ReferenceKind{}
	for _, p := range plans {
		kinds = append(kinds, p.Kind)
	}
	assert.Equal(t, []domain.ReferenceKind{domain.RefProduct, domain.RefStyle, domain.RefKeyframe}, kinds, "priority order: identity first, Keyframe last")
	assert.True(t, plans[0].UseUploaded)
	assert.Contains(t, f.user, "2 image(s) uploaded", "the model knows the user uploaded material")
}

func TestDesign_RejectsPacksThatBreakTheRules(t *testing.T) {
	cases := map[string]string{
		"no keyframe":    `{"references":[{"kind":"style","prompt":"Cold palette"}]}`,
		"unknown kind":   `{"references":[{"kind":"mood","prompt":"x"},{"kind":"keyframe","prompt":"y"}]}`,
		"duplicate kind": `{"references":[{"kind":"style","prompt":"a"},{"kind":"style","prompt":"b"},{"kind":"keyframe","prompt":"c"}]}`,
		"empty prompt":   `{"references":[{"kind":"style","prompt":" "},{"kind":"keyframe","prompt":"c"}]}`,
		"not English":    `{"references":[{"kind":"style","prompt":"冷たい色調"},{"kind":"keyframe","prompt":"c"}]}`,
	}
	for name, answer := range cases {
		_, err := design(answer, artdirector.Input{})
		require.Error(t, err, name)
		assert.ErrorIs(t, err, planning.ErrInvalidPlan, name)
	}
}
