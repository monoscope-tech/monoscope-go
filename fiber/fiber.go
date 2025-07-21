package monoscopefiber

import (
	"context"
	"errors"
	"net/http"

	fiber "github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
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

func Middleware(config Config) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		baseCtx := ctx.UserContext()
		tracer := otel.GetTracerProvider().Tracer(config.ServiceName)
		newCtx, span := tracer.Start(baseCtx, "apitoolkit-http-span")

		msgID := uuid.Must(uuid.NewRandom())
		ctx.Locals(string(apt.CurrentRequestMessageID), msgID)
		errorList := []apt.ATError{}
		ctx.Locals(string(apt.ErrorListCtxKey), &errorList)

		newCtx = context.WithValue(newCtx, apt.ErrorListCtxKey, &errorList)
		newCtx = context.WithValue(newCtx, apt.CurrentRequestMessageID, msgID)
		ctx.SetUserContext(newCtx)

		respHeaders := map[string][]string{}
		for k, v := range ctx.GetRespHeaders() {
			respHeaders[k] = v
		}
		aptConfig := getAptConfig(config)
		defer func() {
			if err := recover(); err != nil {
				if _, ok := err.(error); !ok {
					err = errors.New(err.(string))
				}
				apt.ReportError(ctx.UserContext(), err.(error))
				payload := apt.BuildFastHTTPPayload(apt.GoFiberSDKType,
					ctx.Context(), 500,
					ctx.Request().Body(), ctx.Response().Body(), respHeaders,
					ctx.AllParams(), ctx.Route().Path,
					config.RedactHeaders, config.RedactRequestBody, config.RedactResponseBody,
					errorList,
					msgID,
					nil,
					string(ctx.Context().Referer()),
					aptConfig,
				)
				apt.CreateSpan(payload, aptConfig, span)
				panic(err)
			}
		}()

		err := ctx.Next()
		payload := apt.BuildFastHTTPPayload(apt.GoFiberSDKType,
			ctx.Context(), ctx.Response().StatusCode(),
			ctx.Request().Body(), ctx.Response().Body(), respHeaders,
			ctx.AllParams(), ctx.Route().Path,
			config.RedactHeaders, config.RedactRequestBody, config.RedactResponseBody,
			errorList,
			msgID,
			nil,
			string(ctx.Context().Referer()),
			aptConfig,
		)

		apt.CreateSpan(payload, aptConfig, span)
		return err
	}
}

func ReportError(ctx context.Context, err error) {
	apt.ReportError(ctx, err)
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
