package monoscope

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// outgoing sends one POST through HTTPClient and returns the caller's response, request and the span's attributes.
func outgoing(t *testing.T, url string) (*http.Response, *http.Request, map[string]attribute.Value) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	client := HTTPClient(context.Background())

	req, _ := http.NewRequest(http.MethodPost, url+"/charge?x=1", strings.NewReader(`{"amount":42}`))
	req.Header.Set("Authorization", "Bearer abc")
	req.Header.Set("X-Trace", "t1")
	res, _ := client.Do(req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	attrs := map[string]attribute.Value{}
	for _, kv := range spans[0].Attributes {
		attrs[string(kv.Key)] = kv.Value
	}
	return res, req, attrs
}

func TestOutgoingRedactsHeadersWithoutBreakingTheCaller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	res, req, attrs := outgoing(t, srv.URL)

	for _, k := range []string{"http.request.header.Authorization", "http.response.header.Set-Cookie"} {
		if got := attrs[k].AsStringSlice(); len(got) != 1 || got[0] != redacted {
			t.Errorf("%s = %v, want redacted", k, got)
		}
	}
	if got := res.Header.Get("Set-Cookie"); got != "session=abc" {
		t.Errorf("caller's Set-Cookie = %q, the response handed back must be untouched", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("caller's Authorization = %q, a RoundTripper must not modify the request", got)
	}
	if got := attrs["http.response.status_code"].AsInt64(); got != http.StatusUnauthorized {
		t.Errorf("status = %d", got)
	}
}

// A failed call must still say what was attempted.
func TestOutgoingKeepsRequestDetailsOnNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, _, attrs := outgoing(t, url)

	body, _ := base64.StdEncoding.DecodeString(attrs["http.request.body"].AsString())
	for k, want := range map[string]string{
		"http.request.method": "POST",
		"http.route":          "/charge",
		"http.target":         "/charge?x=1",
		"net.host.name":       strings.TrimPrefix(url, "http://"),
		"http.request.body":   `{"amount":42}`,
	} {
		got := attrs[k].Emit()
		if k == "http.request.body" {
			got = string(body)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got := attrs["http.request.header.X-Trace"].AsStringSlice(); len(got) != 1 || got[0] != "t1" {
		t.Errorf("request headers lost on error: %v", got)
	}
	if got := attrs["http.request.header.Authorization"].AsStringSlice(); len(got) != 1 || got[0] != redacted {
		t.Errorf("Authorization = %v, want redacted", got)
	}
}
