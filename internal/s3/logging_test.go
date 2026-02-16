package s3

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLoggingMiddlewareLogs(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	srv := LoggingMiddleware(inner)
	req := httptest.NewRequest("PUT", "/vault/notes/test.md", nil)
	srv.ServeHTTP(httptest.NewRecorder(), req)

	line := buf.String()
	if !strings.Contains(line, "PUT") {
		t.Errorf("expected log to contain method PUT, got: %s", line)
	}
	if !strings.Contains(line, "/vault/notes/test.md") {
		t.Errorf("expected log to contain path, got: %s", line)
	}
	if !strings.Contains(line, "201") {
		t.Errorf("expected log to contain status 201, got: %s", line)
	}
	if !strings.Contains(line, "ms") {
		t.Errorf("expected log to contain duration in ms, got: %s", line)
	}
}

func TestStatusRecorderCapturesCode(t *testing.T) {
	rec := &statusRecorder{
		ResponseWriter: httptest.NewRecorder(),
		status:         http.StatusOK,
	}
	rec.WriteHeader(http.StatusNotFound)
	if rec.status != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.status)
	}
}

func TestLoggingMiddlewareDefaultStatus(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := LoggingMiddleware(inner)
	req := httptest.NewRequest("GET", "/vault/data.json", nil)
	srv.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(buf.String(), "200") {
		t.Errorf("expected default status 200, got: %s", buf.String())
	}
}

func TestLoggingMiddlewareNoAuthHeader(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := LoggingMiddleware(inner)
	req := httptest.NewRequest("GET", "/vault/test.md", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request")
	srv.ServeHTTP(httptest.NewRecorder(), req)

	line := buf.String()
	if strings.Contains(line, "AWS4-HMAC-SHA256") {
		t.Errorf("log must not contain Authorization header value, got: %s", line)
	}
	if strings.Contains(line, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("log must not contain access key, got: %s", line)
	}
}
