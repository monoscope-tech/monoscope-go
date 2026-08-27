package monoscopegrpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recorded runs one RPC through the interceptor and returns the span's attributes plus
// whatever the interceptor handed back to the caller.
func recorded(
	t *testing.T,
	config Config,
	req any,
	handler grpc.UnaryHandler,
) (map[string]attribute.Value, any, error) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)

	info := &grpc.UnaryServerInfo{FullMethod: "/oteldemo.PaymentService/Charge"}
	resp, err := UnaryServerInterceptor(config)(context.Background(), req, info, handler)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected exactly 1 span, got %d", len(spans))
	}
	if spans[0].Name != "monoscope.http" {
		t.Fatalf("span must be named monoscope.http for the server to lift its bodies, got %q", spans[0].Name)
	}
	attrs := map[string]attribute.Value{}
	for _, kv := range spans[0].Attributes {
		attrs[string(kv.Key)] = kv.Value
	}
	return attrs, resp, err
}

func decodeBody(t *testing.T, attrs map[string]attribute.Value, key string) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(attrs[key].AsString())
	if err != nil {
		t.Fatalf("%s is not base64, so the server cannot lift it: %v", key, err)
	}
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s did not decode to JSON: %v (%q)", key, err, raw)
	}
	return out
}

var capture = Config{
	ServiceName:         "payment",
	CaptureRequestBody:  true,
	CaptureResponseBody: true,
}

func TestCapturesBodiesAndRpcAttributes(t *testing.T) {
	req := map[string]any{"amount": 42}
	handler := func(ctx context.Context, r any) (any, error) {
		return map[string]any{"transactionId": "txn-1"}, nil
	}
	attrs, resp, err := recorded(t, capture, req, handler)

	if err != nil {
		t.Fatalf("interceptor must not introduce an error: %v", err)
	}
	if got := resp.(map[string]any)["transactionId"]; got != "txn-1" {
		t.Fatalf("response must reach the caller unchanged, got %v", got)
	}
	if got := decodeBody(t, attrs, "http.request.body")["amount"]; got != float64(42) {
		t.Errorf("request body not captured, got %v", got)
	}
	if got := decodeBody(t, attrs, "http.response.body")["transactionId"]; got != "txn-1" {
		t.Errorf("response body not captured, got %v", got)
	}
	if got := attrs["rpc.system"].AsString(); got != "grpc" {
		t.Errorf("rpc.system = %q", got)
	}
	if got := attrs["rpc.grpc.status_code"].AsInt64(); got != 0 {
		t.Errorf("a successful RPC is gRPC status 0 (OK), got %d", got)
	}
	if got := attrs["http.route"].AsString(); got != "/oteldemo.PaymentService/Charge" {
		t.Errorf("http.route should be the RPC path, got %q", got)
	}
}

// A gRPC message arrives already decoded, so redaction has to work on the marshalled form of a
// structure rather than on a JSON string the caller handed us.
func TestRedactsSensitiveFieldsInTheRequest(t *testing.T) {
	config := capture
	config.RedactRequestBody = []string{"$.creditCard.creditCardNumber"}
	req := map[string]any{
		"amount":     42,
		"creditCard": map[string]any{"creditCardNumber": "4432-8015-6152-0454"},
	}
	attrs, _, _ := recorded(t, config, req, func(ctx context.Context, r any) (any, error) {
		return map[string]any{}, nil
	})

	raw, _ := base64.StdEncoding.DecodeString(attrs["http.request.body"].AsString())
	if strings.Contains(string(raw), "4432-8015-6152-0454") {
		t.Fatalf("credit card leaked into the captured body: %s", raw)
	}
	if !strings.Contains(string(raw), "42") {
		t.Errorf("redaction removed non-sensitive fields too: %s", raw)
	}
}

// The gRPC code is the reason rpc.grpc.status_code exists: NOT_FOUND and PERMISSION_DENIED
// both flatten to HTTP 500, so without it the distinction is gone.
func TestErrorPathKeepsTheGrpcStatusAndReturnsTheError(t *testing.T) {
	failure := status.Error(codes.PermissionDenied, "card declined")
	attrs, resp, err := recorded(t, capture, map[string]any{}, func(ctx context.Context, r any) (any, error) {
		return nil, failure
	})

	if err != failure {
		t.Fatalf("the handler's own error must reach the caller, got %v", err)
	}
	if resp != nil {
		t.Errorf("expected no response alongside an error, got %v", resp)
	}
	if got := attrs["http.response.status_code"].AsInt64(); got != 500 {
		t.Errorf("http.response.status_code = %d", got)
	}
	if got := attrs["rpc.grpc.status_code"].AsInt64(); got != int64(codes.PermissionDenied) {
		t.Errorf("rpc.grpc.status_code should be PermissionDenied(7), got %d", got)
	}
}

// Capture must never be the reason an RPC fails.
func TestUnmarshalableMessageDegradesToNoBody(t *testing.T) {
	req := map[string]any{"bad": make(chan int)} // channels cannot be marshalled
	attrs, resp, err := recorded(t, capture, req, func(ctx context.Context, r any) (any, error) {
		return map[string]any{"ok": true}, nil
	})

	if err != nil {
		t.Fatalf("an unmarshalable request must not fail the RPC: %v", err)
	}
	if resp.(map[string]any)["ok"] != true {
		t.Errorf("response should still reach the caller")
	}
	if body := attrs["http.request.body"].AsString(); body != "" {
		t.Errorf("expected an empty body rather than garbage, got %q", body)
	}
}

func TestCaptureDisabledRecordsNoBodies(t *testing.T) {
	attrs, _, _ := recorded(t, Config{ServiceName: "payment"}, map[string]any{"secret": "x"},
		func(ctx context.Context, r any) (any, error) { return map[string]any{"y": 1}, nil })

	for _, key := range []string{"http.request.body", "http.response.body"} {
		if got := attrs[key].AsString(); got != "" {
			t.Errorf("%s should be empty when capture is off, got %q", key, got)
		}
	}
}
