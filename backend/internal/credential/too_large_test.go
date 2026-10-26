package credential_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service"
	credentialshandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
)

// tooLargeStore is a readable store whose Set refuses the key for its size, as Keyring does when
// the OS secure storage answers keyring.ErrSetDataTooBig.
type tooLargeStore struct{ *credentialtest.Memory }

func (tooLargeStore) Set(string, string) error { return credential.ErrTooLarge }

// A key the OS store refuses for its size is a 422 about the key, not a 503 that sends the user to
// the key chain (#679).
func TestSaveCredential_KeyTooLargeForTheOSStoreAnswers422(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/credentials/:provider", credentialshandler.Save(credentialshandler.Keys{Svc: service.NewCredentialService(tooLargeStore{credentialtest.New()})}))

	form := url.Values{"api_key": {"sk-within-the-service-limit"}}
	req := httptest.NewRequest(http.MethodPut, "/credentials/kling", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "長すぎます")
	assert.NotContains(t, rec.Body.String(), "キーチェーン")
}

func TestCredentialService_KeyOverTheLimitIsAValidationError(t *testing.T) {
	keyring.MockInit()
	err := service.NewCredentialService(credential.NewKeyring()).Save("kling", strings.Repeat("a", service.MaxAPIKeyBytes+1))
	var invalid *service.ValidationError
	if assert.ErrorAs(t, err, &invalid) {
		assert.Equal(t, "api_key", invalid.Field)
		assert.Contains(t, invalid.Message, "長すぎます")
	}
}
