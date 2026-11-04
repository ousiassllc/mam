package topaz

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// The download URL comes from the status answer, so it passes the same guard as the upload URL: https and a public host, else nothing is requested.
func TestDownload_AURLThatIsNotHTTPSOfAPublicHostIsRefusedBeforeAnyRequest(t *testing.T) {
	c := New(Config{}, credentialtest.New(), refusingDoer{t})
	for _, target := range []string{"http://example.com/out", "https://127.0.0.1/out", "https://localhost/out", "https://192.168.1.1/out", "file:///etc/passwd"} {
		dest := filepath.Join(t.TempDir(), "out.mp4")

		n, err := c.download(context.Background(), "req-1", target, dest)

		require.Error(t, err, target)
		assert.Zero(t, n)
		var te *Error
		require.ErrorAs(t, err, &te, target)
		assert.Equal(t, CodeTransferFailed, te.Code)
		assert.NoFileExists(t, dest)
	}
}

// refusingDoer fails the test when it is asked to send anything.
type refusingDoer struct{ t *testing.T }

func (d refusingDoer) Do(r *http.Request) (*http.Response, error) {
	d.t.Fatalf("a request was sent to %s", r.URL.Host)
	return nil, nil
}
