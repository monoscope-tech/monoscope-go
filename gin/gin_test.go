package monoscopegin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestPanicWithANonErrorValueIsReportedAndRepanicked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	exporter := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	r := gin.New()
	r.Use(Middleware(Config{}))
	r.GET("/", func(*gin.Context) { panic(42) })

	defer func() {
		if got := recover(); got != 42 {
			t.Fatalf("recovered %v, want the handler's own panic value", got)
		}
		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}
		for _, kv := range spans[0].Attributes {
			if kv.Key == "apitoolkit.errors" && !strings.Contains(kv.Value.AsString(), `"root_error_message":"42"`) {
				t.Errorf("apitoolkit.errors = %s, want the panic value", kv.Value.AsString())
			}
		}
	}()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
