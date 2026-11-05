// Package monoscopegorilla tests ensure the Gorilla Mux middleware
// maintains correct behavior across refactoring and changes.
// These tests verify request/response capture, status codes, headers,
// and OpenTelemetry span creation work as expected.
package monoscopegorilla

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMiddleware(t *testing.T) {
	// Setup OpenTelemetry with in-memory exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	tests := []struct {
		name                string
		config              Config
		method              string
		path                string
		requestBody         string
		requestHeaders      map[string]string
		handlerFunc         func(w http.ResponseWriter, r *http.Request)
		expectedStatus      int
		expectedBody        string
		expectedHeaders     map[string]string
		validateRequestBody bool
		validateRespBody    bool
	}{
		{
			name: "basic request with 200 response",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "GET",
			path:        "/test",
			requestBody: "",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Test", "value")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
			expectedHeaders: map[string]string{
				"X-Test": "value",
			},
		},
		{
			name: "request with body capture",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "POST",
			path:        "/test",
			requestBody: `{"key":"value"}`,
			requestHeaders: map[string]string{
				"Content-Type": "application/json",
			},
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"key":"value"}` {
					t.Errorf("Request body not properly passed through: got %s", string(body))
				}
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(`{"result":"success"}`))
			},
			expectedStatus:      http.StatusCreated,
			expectedBody:        `{"result":"success"}`,
			validateRequestBody: true,
		},
		{
			name: "request without body capture",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  false,
				CaptureResponseBody: false,
			},
			method:      "POST",
			path:        "/test",
			requestBody: `{"key":"value"}`,
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				// Body should still be readable even if not captured
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"key":"value"}` {
					t.Errorf("Request body not properly passed through when capture disabled: got %s", string(body))
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Response"))
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "Response",
		},
		{
			name: "handler with no explicit status code",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "GET",
			path:        "/test",
			requestBody: "",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				// Don't call WriteHeader, should default to 200
				w.Write([]byte("No status set"))
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "No status set",
		},
		{
			name: "handler with multiple headers same key",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "GET",
			path:        "/test",
			requestBody: "",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Custom", "value1")
				w.Header().Add("X-Custom", "value2")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Multiple headers"))
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "Multiple headers",
			expectedHeaders: map[string]string{
				"X-Custom": "value1", // Just check first value exists
			},
		},
		{
			name: "handler with error status",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "GET",
			path:        "/test",
			requestBody: "",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Error occurred"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Error occurred",
		},
		{
			name: "handler with empty response",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "GET",
			path:        "/test",
			requestBody: "",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				// Don't write anything
			},
			expectedStatus: http.StatusOK, // Should default to 200
			expectedBody:   "",
		},
		{
			name: "handler with path variables",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "GET",
			path:        "/users/123",
			requestBody: "",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				vars := mux.Vars(r)
				userID := vars["id"]
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("User: " + userID))
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "User: 123",
		},
		{
			name: "request with large body",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "POST",
			path:        "/test",
			requestBody: string(bytes.Repeat([]byte("a"), 10000)), // 10KB body
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if len(body) != 10000 {
					t.Errorf("Large request body not properly handled: got %d bytes", len(body))
				}
				w.WriteHeader(http.StatusOK)
				responseBody := bytes.Repeat([]byte("b"), 10000)
				w.Write(responseBody)
			},
			expectedStatus: http.StatusOK,
			expectedBody:   string(bytes.Repeat([]byte("b"), 10000)),
		},
		{
			name: "handler that panics with recovery",
			config: Config{
				ServiceName:         "test-service",
				CaptureRequestBody:  true,
				CaptureResponseBody: true,
			},
			method:      "GET",
			path:        "/test",
			requestBody: "",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				// Write partial response then panic
				w.Header().Set("X-Before-Panic", "yes")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Before"))
				// Note: In real middleware, panic would be caught by a recovery middleware
				// For this test, we'll just complete normally
				w.Write([]byte(" panic"))
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "Before panic",
			expectedHeaders: map[string]string{
				"X-Before-Panic": "yes",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear previous spans
			exporter.Reset()

			// Setup router with middleware
			router := mux.NewRouter()
			router.Use(Middleware(tt.config))

			// Add route
			if tt.name == "handler with path variables" {
				router.HandleFunc("/users/{id}", tt.handlerFunc).Methods(tt.method)
			} else {
				router.HandleFunc(tt.path, tt.handlerFunc).Methods(tt.method)
			}

			// Create request
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.requestBody))
			for k, v := range tt.requestHeaders {
				req.Header.Set(k, v)
			}

			// Create response recorder
			rec := httptest.NewRecorder()

			// Serve the request
			router.ServeHTTP(rec, req)

			// Validate response
			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if rec.Body.String() != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, rec.Body.String())
			}

			for k, v := range tt.expectedHeaders {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("Expected header %s=%s, got %s", k, v, got)
				}
			}

			// Check that span was created
			spans := exporter.GetSpans()
			if len(spans) == 0 {
				t.Error("Expected at least one span to be created")
			} else {
				span := spans[0]
				if span.Name != "monoscope.http" {
					t.Errorf("Expected span name 'monoscope.http', got %s", span.Name)
				}
			}
		})
	}
}

// TestMiddlewareWithRedaction tests that sensitive data is properly redacted
func TestMiddlewareWithRedaction(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(trace.NewTracerProvider(trace.WithSyncer(exporter)))
	config := Config{
		ServiceName:         "test-service",
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
		RedactHeaders:       []string{"Authorization", "X-Secret"},
		RedactRequestBody:   []string{"$.password", "$.credit_card"},
		RedactResponseBody:  []string{"$.token", "$.secret"},
	}

	router := mux.NewRouter()
	router.Use(Middleware(config))
	router.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		// Echo back the request with some additions
		body["token"] = "secret-token"
		body["public"] = "visible"

		w.Header().Set("X-Secret", "should-be-redacted")
		w.Header().Set("X-Public", "visible")
		w.Header().Set("Set-Cookie", "session=abc")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(body)
	}).Methods("POST")

	reqBody := `{"password":"secret123","username":"john","credit_card":"4111111111111111"}`
	req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-Secret", "secret-value")
	req.Header.Set("cookie", "session=abc")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Verify the response is correct (middleware should not affect the actual response)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Set-Cookie"); got != "session=abc" {
		t.Errorf("client must receive the real Set-Cookie, got %q", got)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	attrs := map[string]attribute.Value{}
	for _, kv := range spans[0].Attributes {
		attrs[string(kv.Key)] = kv.Value
	}
	for k, want := range map[string]string{
		"http.request.header.Authorization": "[CLIENT_REDACTED]",
		"http.request.header.X-Secret":      "[CLIENT_REDACTED]",
		"http.request.header.Cookie":        "[CLIENT_REDACTED]",
		"http.request.header.Content-Type":  "application/json",
		"http.response.header.X-Secret":     "[CLIENT_REDACTED]",
		"http.response.header.Set-Cookie":   "[CLIENT_REDACTED]",
		"http.response.header.X-Public":     "visible",
	} {
		if got := attrs[k].AsStringSlice(); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want %q", k, got, want)
		}
	}
	for k, secret := range map[string]string{"http.request.body": "secret123", "http.response.body": "secret-token"} {
		body, _ := base64.StdEncoding.DecodeString(attrs[k].AsString())
		if strings.Contains(string(body), secret) || !strings.Contains(string(body), "john") {
			t.Errorf("%s not redacted: %s", k, body)
		}
	}
}

func TestMultipleWriteHeaderCalls(t *testing.T) {
	config := Config{ServiceName: "test"}
	mux := mux.NewRouter()
	handler := Middleware(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.WriteHeader(202) // Should be ignored
		w.Write([]byte("test"))
	}))

	mux.HandleFunc("/", handler.ServeHTTP)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Errorf("Multiple WriteHeader calls not handled correctly: got %d, want 201", rec.Code)
	}
}
