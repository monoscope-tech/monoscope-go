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
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// outgoing sends one POST through HTTPClient and returns the caller's response, request, the span and its attributes.
func outgoing(t *testing.T, url string) (*http.Response, *http.Request, tracetest.SpanStub, map[string]attribute.Value) {
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
	return res, req, spans[0], attrs
}

func TestOutgoingRedactsHeadersWithoutBreakingTheCaller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	res, req, _, attrs := outgoing(t, srv.URL)

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
	_, _, span, attrs := outgoing(t, url)

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
	// No response was received, so no status code is invented for it.
	if got := attrs["http.response.status_code"].AsInt64(); got != 0 {
		t.Errorf("status = %d, want 0", got)
	}
	if got := attrs["apitoolkit.errors"].AsString(); !strings.Contains(got, "connection refused") {
		t.Errorf("apitoolkit.errors = %s, want the network error", got)
	}
	if span.Status.Code != codes.Error || len(span.Events) != 1 || span.Events[0].Name != "exception" {
		t.Errorf("status = %v, events = %v, want the error recorded on the span", span.Status, span.Events)
	}
}

func TestOutgoingSpanIsParentedToTheRequestContext(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	exporter := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	clientCtx, clientParent := otel.Tracer("t").Start(context.Background(), "client")
	reqCtx, reqParent := otel.Tracer("t").Start(context.Background(), "request")

	for name, tc := range map[string]struct {
		ctx  context.Context
		want trace.SpanID
	}{
		"request context":                      {reqCtx, reqParent.SpanContext().SpanID()},
		"client context when request has none": {context.Background(), clientParent.SpanContext().SpanID()},
	} {
		t.Run(name, func(t *testing.T) {
			exporter.Reset()
			req, _ := http.NewRequestWithContext(tc.ctx, http.MethodGet, srv.URL, nil)
			res, err := HTTPClient(clientCtx).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			// The parents are never ended, so the outgoing span is the only one exported.
			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("expected 1 span, got %d", len(spans))
			}
			if got := spans[0].Parent.SpanID(); got != tc.want {
				t.Errorf("parent = %v, want %v", got, tc.want)
			}
		})
	}
}
