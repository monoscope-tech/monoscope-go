package monoscopegorilla

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/honeycombio/otel-config-go/otelconfig"
	apt "github.com/monoscope-tech/monoscope-go"
	"go.opentelemetry.io/otel"
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

// GorillaMuxMiddleware is for the gorilla mux routing library and collects request, response parameters and publishes the payload
func Middleware(config Config) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			tracer := otel.GetTracerProvider().Tracer(config.ServiceName)
			newCtx, span := tracer.Start(req.Context(), "apitoolkit-http-span")

			msgID := uuid.Must(uuid.NewRandom())
			newCtx = context.WithValue(newCtx, apt.CurrentRequestMessageID, msgID)

			errorList := []apt.ATError{}
			newCtx = context.WithValue(newCtx, apt.ErrorListCtxKey, &errorList)
			req = req.WithContext(newCtx)

			reqBuf, _ := io.ReadAll(req.Body)
			req.Body.Close()
			req.Body = io.NopCloser(bytes.NewBuffer(reqBuf))

			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, req)

			recRes := rec.Result()
			for k, v := range recRes.Header {
				for _, vv := range v {
					res.Header().Add(k, vv)
				}
			}
			resBody, _ := io.ReadAll(recRes.Body)
			res.WriteHeader(recRes.StatusCode)
			res.Write(resBody)

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

			payload := apt.BuildPayload(apt.GoGorillaMux,
				req, recRes.StatusCode,
				reqBuf, resBody, recRes.Header, vars, pathTmpl,
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

func ConfigureOpenTelemetry(opts ...otelconfig.Option) (func(), error) {
	opts = append([]otelconfig.Option{otelconfig.WithExporterEndpoint("otelcol.apitoolkit.io:4317"), otelconfig.WithExporterInsecure(true)}, opts...)
	return otelconfig.ConfigureOpenTelemetry(opts...)
}

var WithServiceName = otelconfig.WithServiceName
var WithServiceVersion = otelconfig.WithServiceVersion
var WithLogLevel = otelconfig.WithLogLevel
var WithResourceAttributes = otelconfig.WithResourceAttributes
var WithResourceOption = otelconfig.WithResourceOption
var WithPropagators = otelconfig.WithPropagators
var WithErrorHandler = otelconfig.WithErrorHandler
var WithMetricsReportingPeriod = otelconfig.WithMetricsReportingPeriod
var WithMetricsEnabled = otelconfig.WithMetricsEnabled
var WithTracesEnabled = otelconfig.WithTracesEnabled
var WithSpanProcessor = otelconfig.WithSpanProcessor
var WithSampler = otelconfig.WithSampler

func HTTPClient(ctx context.Context, opts ...apt.RoundTripperOption) *http.Client {
	return apt.HTTPClient(ctx, opts...)
}

var WithRedactHeaders = apt.WithRedactHeaders
var WithRedactRequestBody = apt.WithRedactRequestBody
var WithRedactResponseBody = apt.WithRedactResponseBody
