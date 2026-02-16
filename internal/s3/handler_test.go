package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noopSyncer implements Syncer but does nothing.
type noopSyncer struct{}

func (noopSyncer) Trigger() {}

func newTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	return NewHandler(dir, "vault", "", "", "us-east-1", noopSyncer{}), dir
}

func TestPutAndGetObject(t *testing.T) {
	h, _ := newTestHandler(t)

	// PUT
	body := "hello world"
	req := httptest.NewRequest("PUT", "/vault/notes/test.md", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT got status %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("PUT missing ETag header")
	}

	// GET
	req = httptest.NewRequest("GET", "/vault/notes/test.md", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET got status %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != body {
		t.Fatalf("GET body = %q, want %q", got, body)
	}
	if w.Header().Get("Content-Length") == "" {
		t.Fatal("GET missing Content-Length")
	}
}

func TestHeadObject(t *testing.T) {
	h, dir := newTestHandler(t)

	// Create a file
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0644)

	req := httptest.NewRequest("HEAD", "/vault/file.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HEAD got status %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("HEAD missing ETag")
	}
	if w.Header().Get("Content-Length") != "4" {
		t.Fatalf("HEAD Content-Length = %q, want \"4\"", w.Header().Get("Content-Length"))
	}
}

func TestHeadObjectNotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("HEAD", "/vault/nonexistent.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("HEAD got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteObject(t *testing.T) {
	h, dir := newTestHandler(t)

	// Create a file in a subdirectory
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "file.txt"), []byte("data"), 0644)

	req := httptest.NewRequest("DELETE", "/vault/sub/file.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE got status %d, want %d", w.Code, http.StatusNoContent)
	}

	// File should be gone
	if _, err := os.Stat(filepath.Join(sub, "file.txt")); !os.IsNotExist(err) {
		t.Fatal("file should have been deleted")
	}

	// Empty parent dir should be cleaned up
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("empty parent dir should have been removed")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("DELETE", "/vault/nope.txt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE nonexistent got status %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestListObjectsV2(t *testing.T) {
	h, dir := newTestHandler(t)

	// Create some files
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("bbb"), 0644)

	req := httptest.NewRequest("GET", "/vault?list-type=2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("LIST got status %d, want %d", w.Code, http.StatusOK)
	}

	var result ListBucketResult
	if err := xml.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse XML: %v", err)
	}
	if result.KeyCount != 2 {
		t.Fatalf("KeyCount = %d, want 2", result.KeyCount)
	}
}

func TestListObjectsV2WithPrefix(t *testing.T) {
	h, dir := newTestHandler(t)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("bbb"), 0644)

	req := httptest.NewRequest("GET", "/vault?list-type=2&prefix=sub/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var result ListBucketResult
	xml.Unmarshal(w.Body.Bytes(), &result)
	if result.KeyCount != 1 {
		t.Fatalf("KeyCount with prefix = %d, want 1", result.KeyCount)
	}
	if result.Contents[0].Key != "sub/b.txt" {
		t.Fatalf("Key = %q, want %q", result.Contents[0].Key, "sub/b.txt")
	}
}

func TestListObjectsV2MaxKeys(t *testing.T) {
	h, dir := newTestHandler(t)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0644)

	req := httptest.NewRequest("GET", "/vault?list-type=2&max-keys=2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var result ListBucketResult
	xml.Unmarshal(w.Body.Bytes(), &result)
	if result.KeyCount > 2 {
		t.Fatalf("KeyCount = %d, want <= 2", result.KeyCount)
	}
}

func TestCORSOptions(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("OPTIONS", "/vault/test.md", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("OPTIONS got status %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("missing CORS Allow-Origin header")
	}
}

func TestHeadBucket(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("HEAD", "/vault", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HEAD bucket got status %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHeadBucketNotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("HEAD", "/nonexistent", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("HEAD unknown bucket got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)

	// Object-level PATCH
	req := httptest.NewRequest("PATCH", "/vault/test.md", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH got status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}

	// Bucket-level POST
	req = httptest.NewRequest("POST", "/vault", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST bucket got status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestXMLError(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/vault/nonexistent.md", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusNotFound)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("Content-Type = %q, want application/xml", ct)
	}

	var errResp ErrorResponse
	if err := xml.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error XML: %v", err)
	}
	if errResp.Code != "NoSuchKey" {
		t.Fatalf("error code = %q, want NoSuchKey", errResp.Code)
	}
}

func TestAuthRequired(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler(dir, "vault", "testkey", "testsecret", "us-east-1", noopSyncer{})

	req := httptest.NewRequest("GET", "/vault?list-type=2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated request got status %d, want %d", w.Code, http.StatusForbidden)
	}

	var errResp ErrorResponse
	body, _ := io.ReadAll(w.Body)
	xml.Unmarshal(body, &errResp)
	if errResp.Code != "AccessDenied" {
		t.Fatalf("error code = %q, want AccessDenied", errResp.Code)
	}
}

func TestGetObjectNotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/vault/missing.md", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET missing got status %d, want %d", w.Code, http.StatusNotFound)
	}
}
