package monoscope

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type roundTripper struct {
	base http.RoundTripper
	ctx  context.Context
	cfg  *roundTripperConfig
}

func (rt *roundTripper) RoundTrip(req *http.Request) (res *http.Response, err error) {
	defer func() {
		if err != nil {
			ReportError(rt.ctx, err)
		}
	}()

	tracer := otel.GetTracerProvider().Tracer("")
	_, span := tracer.Start(rt.ctx, "monoscope.http", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	// Capture the request body
	reqBodyBytes := []byte{}
	if req.Body != nil {
		reqBodyBytes, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewBuffer(reqBodyBytes))
	}

	// Add a header to all outgoing requests "X-APITOOLKIT-TRACE-PARENT-ID"
	res, err = rt.base.RoundTrip(req)
	var errorList []ATError
	if err != nil {
		// Add the error for the given request payload
		errorList = append(errorList, BuildError(err))
	}

	var payload Payload
	var parentMsgIDPtr *uuid.UUID
	parentMsgID, ok := rt.ctx.Value(CurrentRequestMessageID).(uuid.UUID)
	if ok {
		parentMsgIDPtr = &parentMsgID
	}

	// Capture the response body
	conf := roundTripperConfigToConfig(rt.cfg)
	if res != nil {
		respBodyBytes, _ := io.ReadAll(res.Body)
		res.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))
		payload = BuildPayload(
			GoOutgoing,
			req, res.StatusCode, reqBodyBytes,
			respBodyBytes, res.Header, nil,
			req.URL.Path,
			rt.cfg.RedactHeaders, rt.cfg.RedactRequestBody, rt.cfg.RedactResponseBody,
			errorList,
			uuid.Nil,
			parentMsgIDPtr,
			conf,
		)
		CreateSpan(payload, conf, span)

	} else {
		payload = BuildPayload(
			GoOutgoing,
			req, 503, reqBodyBytes,
			nil, nil, nil,
			req.URL.Path,
			rt.cfg.RedactHeaders, rt.cfg.RedactRequestBody, rt.cfg.RedactResponseBody,
			errorList,
			uuid.Nil,
			parentMsgIDPtr,
			conf,
		)
		CreateSpan(payload, conf, span)

	}
	return res, err
}

func HTTPClient(ctx context.Context, opts ...RoundTripperOption) *http.Client {
	// Run the roundTripperConfig to extract out a httpClient Transport
	cfg := new(roundTripperConfig)
	for _, opt := range opts {
		opt(cfg)
	}

	httpClientV := *http.DefaultClient
	httpClient := &httpClientV
	if cfg.HTTPClient != nil {
		// Use httpClient supplied by user.
		v := *cfg.HTTPClient
		httpClient = &v
	}

	httpClient.Transport = WrapRoundTripper(
		ctx, httpClient.Transport,
		opts...,
	)
	return httpClient
}

type roundTripperConfig struct {
	HTTPClient         *http.Client
	RedactHeaders      []string
	RedactRequestBody  []string
	RedactResponseBody []string
}

type RoundTripperOption func(*roundTripperConfig)

// WithHTTPClient allows you supply your own custom http client
func WithHTTPClient(httpClient *http.Client) RoundTripperOption {
	return func(rc *roundTripperConfig) {
		rc.HTTPClient = httpClient
	}
}

func WithRedactHeaders(headers ...string) RoundTripperOption {
	return func(rc *roundTripperConfig) {
		rc.RedactHeaders = headers
	}
}

func WithRedactRequestBody(fields ...string) RoundTripperOption {
	return func(rc *roundTripperConfig) {
		rc.RedactRequestBody = fields
	}
}

func WithRedactResponseBody(fields ...string) RoundTripperOption {
	return func(rc *roundTripperConfig) {
		rc.RedactResponseBody = fields
	}
}

// WrapRoundTripper returns a new RoundTripper which traces all requests sent
// over the transport.
func WrapRoundTripper(ctx context.Context, rt http.RoundTripper, opts ...RoundTripperOption) http.RoundTripper {
	cfg := new(roundTripperConfig)
	for _, opt := range opts {
		opt(cfg)
	}

	// If no rt is passed in, then use the default standard library transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &roundTripper{
		base: rt,
		ctx:  ctx,
		cfg:  cfg,
	}
}

func roundTripperConfigToConfig(cfg *roundTripperConfig) Config {
	return Config{
		RedactHeaders:       cfg.RedactHeaders,
		RedactRequestBody:   cfg.RedactRequestBody,
		RedactResponseBody:  cfg.RedactResponseBody,
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
	}
}
