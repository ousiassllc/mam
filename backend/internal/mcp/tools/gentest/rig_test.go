package gentest_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func newRig(t *testing.T, opts ...gentesttest.Option) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, opts...)
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		files.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
	})
	return r
}

// serve sends a request to the file endpoint (the tickets of the tools).
func (r *rig) serve(method, url string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url[strings.Index(url, mcpkit.FilesPath):], body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	r.Registrar.Files.Handler().ServeHTTP(rec, req)
	return rec
}

func multipartBody(t *testing.T, field, name string, data []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, name)
	require.NoError(t, err)
	_, _ = part.Write(data)
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

func (r *rig) job(id string) mcpkit.JobStatus {
	r.T.Helper()
	var st mcpkit.JobStatus
	require.Eventually(r.T, func() bool {
		r.CallOK("get_job", map[string]any{"job_id": id}, &st)
		return st.State.Final()
	}, 20*time.Second, 10*time.Millisecond)
	return st
}

func batch(models ...string) map[string]any {
	return map[string]any{
		"media": "image", "models": models, "count": 2,
		"prompts": []map[string]any{{"prompt": "a red fox", "negative_prompt": "blurry"}, {"prompt": "a blue fox"}},
	}
}

// run estimates the batch and runs it with the figures it was shown.
func (r *rig) run(args map[string]any) (mcpgentest.RunOutput, gentest.Estimate) {
	r.T.Helper()
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", run, &out)
	return out, est
}
