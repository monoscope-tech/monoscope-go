package monoscope

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/AsaiYusuke/jsonpath"
	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	GoDefaultSDKType = "GoBuiltIn"
	GoGinSDKType     = "GoGin"
	GoGorillaMux     = "GoGorillaMux"
	GoOutgoing       = "GoOutgoing"
	GoFiberSDKType   = "GoFiber"
)

type ctxKey string

var (
	ErrorListCtxKey         = ctxKey("error-list")
	CurrentRequestMessageID = ctxKey("current-req-msg-id")
	CurrentSpan             = ctxKey("current=apitoolkit-client")
	SpanName                = ctxKey("monoscope.http")
)

// Payload represents request and response details
// FIXME: How would we handle errors from background processes (Not web requests)
type Payload struct {
	RequestHeaders  map[string][]string `json:"request_headers"`
	QueryParams     map[string][]string `json:"query_params"`
	PathParams      map[string]string   `json:"path_params"`
	ResponseHeaders map[string][]string `json:"response_headers"`
	Method          string              `json:"method"`
	SdkType         string              `json:"sdk_type"`
	Host            string              `json:"host"`
	RawURL          string              `json:"raw_url"`
	Referer         string              `json:"referer"`
	URLPath         string              `json:"url_path"`
	ResponseBody    []byte              `json:"response_body"`
	RequestBody     []byte              `json:"request_body"`
	ProtoMinor      int                 `json:"proto_minor"`
	StatusCode      int                 `json:"status_code"`
	ProtoMajor      int                 `json:"proto_major"`
	Errors          []ATError           `json:"errors"`
	ServiceVersion  *string             `json:"service_version"`
	Tags            []string            `json:"tags"`
	MsgID           string              `json:"msg_id"`
	ParentID        *string             `json:"parent_id"`
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

func CreateSpan(payload Payload, config Config, span trace.Span) {
	atErrors, _ := json.Marshal(payload.Errors)
	queryParams, _ := json.Marshal(payload.QueryParams)
	pathParams, _ := json.Marshal(payload.PathParams)
	requestBody := []byte{}
	if config.CaptureRequestBody {
		requestBody = payload.RequestBody
	}
	responseBody := []byte{}
	if config.CaptureResponseBody {
		responseBody = payload.ResponseBody
	}
	attrs := []attribute.KeyValue{
		attribute.String("apitoolkit.service_version", config.ServiceVersion),
		attribute.String("net.host.name", payload.Host),
		attribute.String("http.route", payload.URLPath),
		attribute.String("http.request.method", payload.Method),
		attribute.Int("http.response.status_code", payload.StatusCode),
		attribute.String("http.request.query_params", string(queryParams)),
		attribute.String("http.target", payload.RawURL),
		attribute.String("http.request.path_params", string(pathParams)),
		attribute.String("apitoolkit.sdk_type", payload.SdkType),
		attribute.String("http.request.body", base64.StdEncoding.EncodeToString(requestBody)),
		attribute.String("http.response.body", base64.StdEncoding.EncodeToString(responseBody)),
		attribute.String("apitoolkit.errors", string(atErrors)),
		attribute.StringSlice("apitoolkit.tags", payload.Tags),
	}
	span.SetAttributes(attrs...)

	for key, value := range payload.RequestHeaders {
		span.SetAttributes(attribute.KeyValue{Key: attribute.Key("http.request.header." + key), Value: attribute.StringSliceValue(value)})
	}

	for key, value := range payload.ResponseHeaders {
		span.SetAttributes(attribute.KeyValue{Key: attribute.Key("http.response.header." + key), Value: attribute.StringSliceValue(value)})
	}
	if payload.MsgID != "" {
		span.SetAttributes(attribute.String("apitoolkit.msg_id", payload.MsgID))

	}

}

func RedactJSON(data []byte, redactList []string) []byte {
	config := jsonpath.Config{}
	config.SetAccessorMode()

	var src interface{}
	json.Unmarshal(data, &src)

	for _, key := range redactList {
		output, _ := jsonpath.Retrieve(key, src, config)
		for _, v := range output {
			accessor, ok := v.(jsonpath.Accessor)
			if ok {
				accessor.Set("[CLIENT_REDACTED]")
			}
		}
	}
	dataJSON, _ := json.Marshal(src)
	return dataJSON
}

func RedactHeaders(headers map[string][]string, redactList []string) map[string][]string {
	for k := range headers {
		if find(redactList, k) {
			headers[k] = []string{"[CLIENT_REDACTED]"}
		}
	}
	return headers
}

func find(haystack []string, needle string) bool {
	for _, hay := range haystack {
		if strings.EqualFold(hay, needle) {
			return true
		}
	}
	return false
}

func BuildPayload(SDKType string, req *http.Request,
	statusCode int, reqBody []byte, respBody []byte, respHeader map[string][]string,
	pathParams map[string]string, urlPath string,
	redactHeadersList,
	redactRequestBodyList, redactResponseBodyList []string,
	errorList []ATError,
	msgID uuid.UUID,
	parentID *uuid.UUID,
	config Config,
) Payload {
	if req == nil || req.URL == nil {
		// Early return with empty payload to prevent any nil pointer panics
		if config.Debug {
			log.Println("APIToolkit: nil request or url while building payload.")
		}
		return Payload{}
	}

	redactedHeaders := []string{"password", "Authorization", "Cookies"}
	for _, v := range redactHeadersList {
		redactedHeaders = append(redactedHeaders, strings.ToLower(v))
	}

	var parentIDVal *string
	if parentID != nil {
		parentIDStr := (*parentID).String()
		parentIDVal = &parentIDStr
	}

	var serviceVersion *string
	if config.ServiceVersion != "" {
		serviceVersion = &config.ServiceVersion
	}
	msgIDStr := ""
	if msgID != uuid.Nil {
		msgIDStr = msgID.String()
	}
	return Payload{
		Host:            req.Host,
		Method:          req.Method,
		PathParams:      pathParams,
		ProtoMajor:      req.ProtoMajor,
		ProtoMinor:      req.ProtoMinor,
		QueryParams:     req.URL.Query(),
		RawURL:          req.URL.RequestURI(),
		Referer:         req.Referer(),
		RequestBody:     RedactJSON(reqBody, redactRequestBodyList),
		RequestHeaders:  RedactHeaders(req.Header, redactedHeaders),
		ResponseBody:    RedactJSON(respBody, redactResponseBodyList),
		ResponseHeaders: RedactHeaders(respHeader, redactedHeaders),
		SdkType:         SDKType,
		StatusCode:      statusCode,
		URLPath:         urlPath,
		Errors:          errorList,
		ServiceVersion:  serviceVersion,
		Tags:            config.Tags,
		MsgID:           msgIDStr,
		ParentID:        parentIDVal,
	}
}

func BuildFastHTTPPayload(SDKType string, req *fasthttp.RequestCtx,
	statusCode int, reqBody []byte, respBody []byte, respHeader map[string][]string,
	pathParams map[string]string, urlPath string,
	redactHeadersList,
	redactRequestBodyList, redactResponseBodyList []string,
	errorList []ATError,
	msgID uuid.UUID,
	parentID *uuid.UUID,
	referer string,
	config Config,
) Payload {
	if req == nil || req.URI() == nil {
		// Early return with empty payload to prevent any nil pointer panics
		if config.Debug {
			log.Println("APIToolkit: nil request or client or url while building payload.")
		}
		return Payload{}
	}

	queryParams := map[string][]string{}
	req.QueryArgs().VisitAll(func(key, value []byte) {
		queryParams[string(key)] = []string{string(value)}
	})

	reqHeaders := map[string][]string{}
	req.Request.Header.VisitAll(func(key, value []byte) {
		reqHeaders[string(key)] = []string{string(value)}
	})

	redactedHeaders := []string{"password", "Authorization", "Cookies"}
	for _, v := range redactHeadersList {
		redactedHeaders = append(redactedHeaders, strings.ToLower(v))
	}

	var parentIDVal *string
	if parentID != nil {
		parentIDStr := (*parentID).String()
		parentIDVal = &parentIDStr
	}

	var serviceVersion *string
	if config.ServiceVersion != "" {
		serviceVersion = &config.ServiceVersion
	}

	return Payload{
		Host:            string(req.Host()),
		Method:          string(req.Method()),
		PathParams:      pathParams,
		ProtoMajor:      1, // req.ProtoMajor,
		ProtoMinor:      1, // req.ProtoMinor,
		QueryParams:     queryParams,
		RawURL:          string(req.RequestURI()),
		Referer:         referer,
		RequestBody:     RedactJSON(reqBody, redactRequestBodyList),
		RequestHeaders:  RedactHeaders(reqHeaders, redactedHeaders),
		ResponseBody:    RedactJSON(respBody, redactResponseBodyList),
		ResponseHeaders: RedactHeaders(respHeader, redactedHeaders),
		SdkType:         SDKType,
		StatusCode:      statusCode,
		URLPath:         urlPath,
		Errors:          errorList,
		ServiceVersion:  serviceVersion,
		Tags:            config.Tags,
		MsgID:           msgID.String(),
		ParentID:        parentIDVal,
	}
}
