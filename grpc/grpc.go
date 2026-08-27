// Package monoscopegrpc captures gRPC request and response payloads for Monoscope.
//
// Every other integration in this SDK is an HTTP middleware, which leaves a gRPC service with
// no payloads at all — and in Go, gRPC is frequently a service's only protocol.
package monoscopegrpc

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	apt "github.com/monoscope-tech/monoscope-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Config struct {
	Debug               bool
	ServiceVersion      string
	ServiceName         string
	RedactHeaders       []string
	RedactRequestBody   []string
	RedactResponseBody  []string
	Tags                []string
	CaptureRequestBody  bool
	CaptureResponseBody bool
}

func ReportError(ctx context.Context, err error) {
	apt.ReportError(ctx, err)
}

// marshal renders a gRPC message as JSON.
//
// protojson rather than encoding/json for anything that is a proto.Message: it applies the
// field names declared in the .proto and, importantly, renders 64-bit fields as strings. Plain
// encoding/json on a generated struct leaks the generator's internal state and gives field
// names that will not match a redaction path the user wrote against their schema.
//
// Best-effort by design — capture must never be the reason an RPC fails, so a message that
// cannot be marshalled yields no body rather than an error on the request path.
func marshal(msg any) []byte {
	if pm, ok := msg.(proto.Message); ok {
		if b, err := protojson.Marshal(pm); err == nil {
			return b
		}
		return nil
	}
	if b, err := json.Marshal(msg); err == nil {
		return b
	}
	return nil
}

// httpStatusFor maps a gRPC outcome onto the HTTP status the span contract carries.
//
// It is lossy, and deliberately so: the server lifts payloads into the Req/Resp Body tabs
// based on an HTTP-shaped span, so those fields have to be present. The unflattened gRPC code
// is recorded separately as rpc.grpc.status_code, so nothing is actually lost.
func httpStatusFor(err error) int {
	if err == nil {
		return 200
	}
	return 500
}

// UnaryServerInterceptor captures the request and response payloads of every unary RPC.
//
//	server := grpc.NewServer(grpc.UnaryInterceptor(
//	    monoscopegrpc.UnaryServerInterceptor(monoscopegrpc.Config{
//	        ServiceName:        "payment",
//	        CaptureRequestBody: true,
//	        RedactRequestBody:  []string{"$.creditCard.creditCardNumber"},
//	    }),
//	))
//
// Streaming RPCs are not covered: there is no single request or response message to capture,
// so a stream interceptor would record nothing while looking like it worked. Use
// [UnaryServerInterceptor] for unary methods and instrument streams by hand if you need them.
func UnaryServerInterceptor(config Config) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		tracer := otel.GetTracerProvider().Tracer(config.ServiceName)
		ctx, span := tracer.Start(ctx, "monoscope.http", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		msgID := uuid.New()
		errorList := []apt.ATError{}
		ctx = context.WithValue(ctx, apt.CurrentRequestMessageID, msgID)
		ctx = context.WithValue(ctx, apt.ErrorListCtxKey, &errorList)
		ctx = context.WithValue(ctx, apt.CurrentSpan, span)

		resp, err := handler(ctx, req)

		// Redaction is skipped when there is nothing to redact: RedactJSON turns nil into the
		// literal `null`, which would be captured as a body of "null" rather than no body —
		// indistinguishable, to a reader, from a message that genuinely was null.
		redact := func(msg any, paths []string) []byte {
			if b := marshal(msg); b != nil {
				return apt.RedactJSON(b, paths)
			}
			return nil
		}

		var reqBody, respBody []byte
		if config.CaptureRequestBody {
			reqBody = redact(req, config.RedactRequestBody)
		}
		if config.CaptureResponseBody && err == nil {
			respBody = redact(resp, config.RedactResponseBody)
		}

		// FullMethod ("/pkg.Service/Method") is the route: it identifies the method rather than
		// the individual call, which is what groups calls together in the UI.
		route := ""
		if info != nil {
			route = info.FullMethod
		}

		apt.CreateSpan(apt.Payload{
			Host:            config.ServiceName,
			Method:          "POST", // gRPC rides on HTTP/2 POST
			URLPath:         route,
			RawURL:          route,
			SdkType:         "GoGrpc",
			StatusCode:      httpStatusFor(err),
			RequestBody:     reqBody,
			ResponseBody:    respBody,
			RequestHeaders:  map[string][]string{},
			ResponseHeaders: map[string][]string{},
			QueryParams:     map[string][]string{},
			PathParams:      map[string]string{},
			Errors:          errorList,
			Tags:            config.Tags,
			MsgID:           msgID.String(),
		}, apt.Config{
			Debug:               config.Debug,
			ServiceVersion:      config.ServiceVersion,
			ServiceName:         config.ServiceName,
			Tags:                config.Tags,
			CaptureRequestBody:  config.CaptureRequestBody,
			CaptureResponseBody: config.CaptureResponseBody,
		}, span)

		// Recorded alongside the HTTP-shaped fields above, not instead of them. A gRPC status
		// says more than ok-or-not — NOT_FOUND and RESOURCE_EXHAUSTED both flatten to 500 — so
		// the real code is kept, ready for the UI to filter on it natively.
		span.SetAttributes(
			attribute.String("rpc.system", "grpc"),
			attribute.String("rpc.method", route),
			attribute.Int("rpc.grpc.status_code", int(status.Code(err))),
		)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}

		// The RPC's own outcome is handed back untouched.
		return resp, err
	}
}
