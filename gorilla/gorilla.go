// Package monoscopegorilla provides middleware and helpers to instrument
// Gorilla Mux HTTP servers with Monoscope telemetry and OpenTelemetry tracing.
package monoscopegorilla

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/honeycombio/otel-config-go/otelconfig"
	apt "github.com/monoscope-tech/monoscope-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Config holds middleware configuration for request/response capture,
// telemetry, and Monoscope reporting.
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

// ReportError reports an error to Monoscope using the given context.
func ReportError(ctx context.Context, err error) {
	apt.ReportError(ctx, err)
}

// Middleware returns a Gorilla Mux middleware handler that:
// - Starts an OpenTelemetry server span
// - Optionally captures the request body
// - Optionally captures the response body
// - Reports the request/response and errors to Monoscope
func Middleware(config Config) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			tracer := otel.GetTracerProvider().Tracer(config.ServiceName)
			newCtx, span := tracer.Start(req.Context(), "monoscope.http", trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			msgID := uuid.New()
			newCtx = context.WithValue(newCtx, apt.CurrentRequestMessageID, msgID)

			errorList := []apt.ATError{}
			newCtx = context.WithValue(newCtx, apt.ErrorListCtxKey, &errorList)
			req = req.WithContext(newCtx)

			var reqBuf []byte
			if config.CaptureRequestBody {
				var err error
				reqBuf, err = io.ReadAll(req.Body)
				if err != nil {
					apt.ReportError(newCtx, err)
				}
				req.Body.Close()
				req.Body = io.NopCloser(bytes.NewBuffer(reqBuf))
			}

			rec := &responseRecorder{ResponseWriter: res, body: &bytes.Buffer{}, captureBody: config.CaptureResponseBody}
			next.ServeHTTP(rec, req)

			var resBody []byte
			if config.CaptureResponseBody {
				resBody = rec.body.Bytes()
			}
			statusCode := rec.StatusCode()

			route := mux.CurrentRoute(req)
			pathTmpl, _ := route.GetPathTemplate()
			vars := mux.Vars(req)

			aptConfig := apt.Config{
				ServiceName:         config.ServiceName,
				ServiceVersion:      config.ServiceVersion,
				Tags:                config.Tags,
				Debug:               config.Debug,
				CaptureRequestBody:  config.CaptureRequestBody,
				CaptureResponseBody: config.CaptureResponseBody,
				RedactHeaders:       config.RedactHeaders,
				RedactRequestBody:   config.RedactRequestBody,
				RedactResponseBody:  config.RedactResponseBody,
			}

			payload := apt.BuildPayload(
				apt.GoGorillaMux,
				req, statusCode,
				reqBuf, resBody,
				res.Header(), vars, pathTmpl,
				config.RedactHeaders, config.RedactRequestBody, config.RedactResponseBody,
				errorList,
				msgID,
				nil,
				aptConfig,
			)
			apt.CreateSpan(payload, aptConfig, span)
		})
	}
}

// responseRecorder wraps an http.ResponseWriter to capture the status code
// and response body for telemetry reporting. It ensures empty responses
// default to 200 OK.
type responseRecorder struct {
	http.ResponseWriter
	body        *bytes.Buffer
	statusCode  int
	status      bool
	captureBody bool
}

// WriteHeader captures the status code and writes headers to the real ResponseWriter.
func (r *responseRecorder) WriteHeader(code int) {
	if !r.status {
		r.status = true
		r.statusCode = code
		r.ResponseWriter.WriteHeader(code)
	}
}

// Write captures response body and ensures WriteHeader is called with 200 if not already.
func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.captureBody {
		r.body.Write(b)
	}
	if !r.status {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// StatusCode returns the actual status code, defaulting to 200 for empty responses.
func (r *responseRecorder) StatusCode() int {
	if r.statusCode == 0 {
		return http.StatusOK
	}
	return r.statusCode
}

// ConfigureOpenTelemetry initializes OpenTelemetry with default options and any additional options.
// Returns a shutdown function to flush telemetry and an error if initialization fails.
func ConfigureOpenTelemetry(opts ...otelconfig.Option) (func(), error) {
	defaultOpts := []otelconfig.Option{
		otelconfig.WithExporterEndpoint("otelcol.apitoolkit.io:4317"),
		otelconfig.WithExporterInsecure(true),
	}
	opts = append(defaultOpts, opts...)
	return otelconfig.ConfigureOpenTelemetry(opts...)
}

// Aliases for OpenTelemetry configuration helpers for convenience.
var (
	WithServiceName            = otelconfig.WithServiceName
	WithServiceVersion         = otelconfig.WithServiceVersion
	WithLogLevel               = otelconfig.WithLogLevel
	WithResourceAttributes     = otelconfig.WithResourceAttributes
	WithResourceOption         = otelconfig.WithResourceOption
	WithPropagators            = otelconfig.WithPropagators
	WithErrorHandler           = otelconfig.WithErrorHandler
	WithMetricsReportingPeriod = otelconfig.WithMetricsReportingPeriod
	WithMetricsEnabled         = otelconfig.WithMetricsEnabled
	WithTracesEnabled          = otelconfig.WithTracesEnabled
	WithSpanProcessor          = otelconfig.WithSpanProcessor
	WithSampler                = otelconfig.WithSampler
)

// HTTPClient returns an instrumented HTTP client using Monoscope's apt library.
func HTTPClient(ctx context.Context, opts ...apt.RoundTripperOption) *http.Client {
	return apt.HTTPClient(ctx, opts...)
}

// Aliases for Monoscope request/response redaction helpers.
var (
	WithRedactHeaders      = apt.WithRedactHeaders
	WithRedactRequestBody  = apt.WithRedactRequestBody
	WithRedactResponseBody = apt.WithRedactResponseBody
)
