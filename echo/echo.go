package monoscopeecho

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/google/uuid"
	"github.com/honeycombio/otel-config-go/otelconfig"
	"github.com/labstack/echo/v4"
	apt "github.com/monoscope-tech/monoscope-go"
	"go.opentelemetry.io/otel"
)

// bodyDumpResponseWriter use to preserve the http response body during request processing
type echoBodyLogWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *echoBodyLogWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
}

func (w *echoBodyLogWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func (w *echoBodyLogWriter) Flush() {
	w.ResponseWriter.(http.Flusher).Flush()
}

func (w *echoBodyLogWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

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

// EchoMiddleware middleware for echo framework, collects requests, response and publishes the payload
func Middleware(config Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) (err error) {
			tracer := otel.GetTracerProvider().Tracer(config.ServiceName)
			newCtx, span := tracer.Start(ctx.Request().Context(), "apitoolkit-http-span")

			msgID := uuid.Must(uuid.NewRandom())
			ctx.Set(string(apt.CurrentRequestMessageID), msgID)

			errorList := []apt.ATError{}
			ctx.Set(string(apt.ErrorListCtxKey), &errorList)
			newCtx = context.WithValue(newCtx, apt.ErrorListCtxKey, &errorList)
			newCtx = context.WithValue(newCtx, apt.CurrentRequestMessageID, msgID)

			// add span context to the request context
			ctx.SetRequest(ctx.Request().WithContext(newCtx))

			var reqBuf []byte
			// safely read request body
			if ctx.Request().Body != nil {
				reqBuf, _ = io.ReadAll(ctx.Request().Body)
			}
			ctx.Request().Body = io.NopCloser(bytes.NewBuffer(reqBuf))
			// create a MultiWriter that streams the response body into resBody
			resBody := new(bytes.Buffer)
			mw := io.MultiWriter(ctx.Response().Writer, resBody)
			writer := &echoBodyLogWriter{Writer: mw, ResponseWriter: ctx.Response().Writer}
			ctx.Response().Writer = writer
			pathParams := map[string]string{}
			for _, paramName := range ctx.ParamNames() {
				pathParams[paramName] = ctx.Param(paramName)
			}
			aptConfig := apt.Config{
				ServiceName:         config.ServiceName,
				ServiceVersion:      config.ServiceVersion,
				Tags:                config.Tags,
				CaptureRequestBody:  config.CaptureRequestBody,
				CaptureResponseBody: config.CaptureResponseBody,
				RedactHeaders:       config.RedactHeaders,
				RedactRequestBody:   config.RedactRequestBody,
				RedactResponseBody:  config.RedactResponseBody,
			}

			defer func() {
				if err := recover(); err != nil {
					if _, ok := err.(error); !ok {
						err = errors.New(err.(string))
					}
					apt.ReportError(ctx.Request().Context(), err.(error))
					payload := apt.BuildPayload(apt.GoDefaultSDKType,
						ctx.Request(), 500,
						reqBuf, resBody.Bytes(), ctx.Response().Header().Clone(),
						pathParams, ctx.Path(),
						config.RedactHeaders, config.RedactRequestBody, config.RedactResponseBody,
						errorList,
						msgID,
						nil,
						aptConfig,
					)
					apt.CreateSpan(payload, aptConfig, span)
					panic(err)
				}
			}()

			// pass on request handling
			err = next(ctx)

			// proceed post-response processing
			payload := apt.BuildPayload(apt.GoDefaultSDKType,
				ctx.Request(), ctx.Response().Status,
				reqBuf, resBody.Bytes(), ctx.Response().Header().Clone(),
				pathParams, ctx.Path(),
				config.RedactHeaders, config.RedactRequestBody, config.RedactResponseBody,
				errorList,
				msgID,
				nil,
				aptConfig,
			)
			apt.CreateSpan(payload, aptConfig, span)
			return err
		}
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
