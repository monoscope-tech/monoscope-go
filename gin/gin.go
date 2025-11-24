package monoscopegin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/honeycombio/otel-config-go/otelconfig"
	apt "github.com/monoscope-tech/monoscope-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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

type ginBodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *ginBodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *ginBodyLogWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func ReportError(ctx context.Context, err error) {
	apt.ReportError(ctx, err)
}

func Middleware(config Config) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		newCtx := ctx.Request.Context()
		tracer := otel.GetTracerProvider().Tracer(config.ServiceName)
		newCtx, span := tracer.Start(newCtx, "monoscope.http", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		msgID := uuid.New()
		ctx.Set(string(apt.CurrentRequestMessageID), msgID)
		errorList := []apt.ATError{}
		ctx.Set(string(apt.ErrorListCtxKey), &errorList)
		newCtx = context.WithValue(newCtx, apt.ErrorListCtxKey, &errorList)
		newCtx = context.WithValue(newCtx, apt.CurrentRequestMessageID, msgID)
		ctx.Request = ctx.Request.WithContext(newCtx)

		reqByteBody, _ := io.ReadAll(ctx.Request.Body)
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(reqByteBody))

		blw := &ginBodyLogWriter{body: bytes.NewBuffer([]byte{}), ResponseWriter: ctx.Writer}
		ctx.Writer = blw

		pathParams := map[string]string{}
		for _, param := range ctx.Params {
			pathParams[param.Key] = param.Value
		}
		aptConfig := getAptConfig(config)

		defer func() {
			if err := recover(); err != nil {
				if _, ok := err.(error); !ok {
					err = errors.New(err.(string))
				}
				apt.ReportError(ctx.Request.Context(), err.(error))
				payload := apt.BuildPayload(apt.GoGinSDKType,
					ctx.Request, 500,
					reqByteBody, blw.body.Bytes(), ctx.Writer.Header().Clone(),
					pathParams, ctx.FullPath(),
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
		ctx.Next()
		payload := apt.BuildPayload(apt.GoGinSDKType,
			ctx.Request, ctx.Writer.Status(),
			reqByteBody, blw.body.Bytes(), ctx.Writer.Header().Clone(),
			pathParams, ctx.FullPath(),
			config.RedactHeaders, config.RedactRequestBody, config.RedactResponseBody,
			errorList,
			msgID,
			nil,
			aptConfig,
		)
		if config.Debug {
			log.Println(payload)
		}
		apt.CreateSpan(payload, aptConfig, span)

	}
}

func getAptConfig(config Config) apt.Config {
	return apt.Config{
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
}

func ConfigureOpenTelemetry(opts ...otelconfig.Option) (func(), error) {
	opts = append([]otelconfig.Option{otelconfig.WithExporterEndpoint("otelcol.apitoolkit.io:4317"), otelconfig.WithExporterInsecure(true)}, opts...)
	return otelconfig.ConfigureOpenTelemetry(opts...)
}

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

func HTTPClient(ctx context.Context, opts ...apt.RoundTripperOption) *http.Client {
	return apt.HTTPClient(ctx, opts...)
}

var (
	WithRedactHeaders      = apt.WithRedactHeaders
	WithRedactRequestBody  = apt.WithRedactRequestBody
	WithRedactResponseBody = apt.WithRedactResponseBody
)
