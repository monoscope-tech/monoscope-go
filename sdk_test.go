package monoscope

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
)

const redacted = "[CLIENT_REDACTED]"

// Every builder must redact the default credential headers and the user's list, matching names
// case-insensitively, without touching the live header maps it was handed.
func TestBuildPayloadRedactsHeaders(t *testing.T) {
	respHeaders := http.Header{"Set-Cookie": {"session=abc"}, "X-Request-Id": {"r1"}}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	for k, v := range map[string]string{"Authorization": "Bearer abc", "Cookie": "session=abc", "Proxy-Authorization": "Basic abc", "X-Api-Key": "k", "Accept": "*/*"} {
		req.Header.Set(k, v)
	}
	std := BuildPayload(GoDefaultSDKType, req, 200, nil, nil, respHeaders, nil, "/x",
		[]string{"x-api-key"}, nil, nil, nil, uuid.Nil, nil, Config{})

	fctx := &fasthttp.RequestCtx{}
	fctx.Request.SetRequestURI("/x")
	for k, v := range map[string]string{"authorization": "Bearer abc", "cookie": "session=abc", "proxy-authorization": "Basic abc", "x-api-key": "k", "accept": "*/*"} {
		fctx.Request.Header.Set(k, v)
	}
	fast := BuildFastHTTPPayload(GoFiberSDKType, fctx, 200, nil, nil, respHeaders, nil, "/x",
		[]string{"X-API-KEY"}, nil, nil, nil, uuid.Nil, nil, "", Config{})

	for name, p := range map[string]Payload{"net/http": std, "fasthttp": fast} {
		for k, v := range p.RequestHeaders {
			if want := map[bool]string{true: "*/*", false: redacted}[http.CanonicalHeaderKey(k) == "Accept"]; v[0] != want {
				t.Errorf("%s: request header %s = %q, want %q", name, k, v[0], want)
			}
		}
		if got := p.ResponseHeaders["Set-Cookie"][0]; got != redacted {
			t.Errorf("%s: response Set-Cookie = %q, want redacted", name, got)
		}
		if got := p.ResponseHeaders["X-Request-Id"][0]; got != "r1" {
			t.Errorf("%s: X-Request-Id = %q, want it kept", name, got)
		}
	}
	if req.Header.Get("Authorization") != "Bearer abc" || respHeaders.Get("Set-Cookie") != "session=abc" {
		t.Errorf("redaction mutated the live headers: %v %v", req.Header, respHeaders)
	}
}
