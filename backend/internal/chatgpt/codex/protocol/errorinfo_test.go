package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

func TestErrorInfoDecodesBothForms(t *testing.T) {
	cases := []struct {
		in         string
		kind       string
		httpStatus int
	}{
		{`"usageLimitExceeded"`, protocol.ErrUsageLimitExceeded, 0},
		{`{"httpConnectionFailed":{"httpStatusCode":502}}`, protocol.ErrHTTPConnectionFailed, 502},
		{`{"responseStreamDisconnected":{"httpStatusCode":null}}`, protocol.ErrResponseStreamDisconnected, 0},
		{`{"responseStreamConnectionFailed":{}}`, protocol.ErrResponseStreamConnectFailed, 0},
		{`null`, "", 0},
		{`{"a":{},"b":{}}`, protocol.ErrOther, 0},
		{`42`, protocol.ErrOther, 0},
	}
	for _, c := range cases {
		var e protocol.ErrorInfo
		if err := json.Unmarshal([]byte(c.in), &e); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if e.Kind != c.kind || e.HTTPStatus != c.httpStatus {
			t.Errorf("%s: got %+v, want kind %q status %d", c.in, e, c.kind, c.httpStatus)
		}
	}
}

func TestTurnErrorWithoutInfoDecodes(t *testing.T) {
	var n protocol.ErrorNotification
	if err := json.Unmarshal([]byte(`{"threadId":"t","turnId":"u","willRetry":true,"error":{"message":"m"}}`), &n); err != nil {
		t.Fatal(err)
	}
	if !n.WillRetry || n.Error.Message != "m" || n.Error.CodexErrorInfo.Kind != "" {
		t.Fatalf("%+v", n)
	}
}
