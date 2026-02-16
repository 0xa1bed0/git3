package s3

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSortQueryString(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"a=1", "a=1"},
		{"b=2&a=1", "a=1&b=2"},
		{"z=3&a=1&m=2", "a=1&m=2&z=3"},
	}
	for _, tt := range tests {
		got := sortQueryString(tt.input)
		if got != tt.want {
			t.Errorf("sortQueryString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHmacSHA256(t *testing.T) {
	key := []byte("secret")
	data := []byte("hello")
	result := hmacSHA256(key, data)
	got := hex.EncodeToString(result)
	// Known HMAC-SHA256("secret", "hello")
	want := "88aab3ede8d3adf94d26ab90d3bafd4a2083070c3bcce9c014ee04a443847c0b"
	if got != want {
		t.Errorf("hmacSHA256 = %q, want %q", got, want)
	}
}

func TestHashSHA256(t *testing.T) {
	got := hashSHA256([]byte("hello"))
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("hashSHA256 = %q, want %q", got, want)
	}
}

func TestDeriveSigningKey(t *testing.T) {
	key := deriveSigningKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20130524", "us-east-1", "s3")
	if len(key) != 32 {
		t.Fatalf("signing key length = %d, want 32", len(key))
	}
}

func TestSigV4VerifyEmptyHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/vault", nil)
	if sigV4Verify(req, "key", "secret", "us-east-1") {
		t.Fatal("expected false for empty auth header")
	}
}

func TestSigV4VerifyBadPrefix(t *testing.T) {
	req := httptest.NewRequest("GET", "/vault", nil)
	req.Header.Set("Authorization", "Bearer token123")
	if sigV4Verify(req, "key", "secret", "us-east-1") {
		t.Fatal("expected false for non-AWS4 auth")
	}
}

func TestSigV4VerifyMissingFields(t *testing.T) {
	req := httptest.NewRequest("GET", "/vault", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=key/20230101/us-east-1/s3/aws4_request")
	if sigV4Verify(req, "key", "secret", "us-east-1") {
		t.Fatal("expected false for missing SignedHeaders/Signature")
	}
}

func TestSigV4VerifyWrongAccessKey(t *testing.T) {
	req := httptest.NewRequest("GET", "/vault", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=wrongkey/20230101/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=abc123")
	if sigV4Verify(req, "key", "secret", "us-east-1") {
		t.Fatal("expected false for wrong access key")
	}
}

func TestSigV4VerifyWrongRegion(t *testing.T) {
	req := httptest.NewRequest("GET", "/vault", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=key/20230101/eu-west-1/s3/aws4_request, SignedHeaders=host, Signature=abc123")
	if sigV4Verify(req, "key", "secret", "us-east-1") {
		t.Fatal("expected false for wrong region")
	}
}

func TestSigV4VerifyValidSignature(t *testing.T) {
	// Build a request and sign it ourselves, then verify
	accessKey := "AKIAIOSFODNN7EXAMPLE"
	secretKey := "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
	region := "us-east-1"
	dateStamp := "20230101"
	amzDate := "20230101T000000Z"

	req := httptest.NewRequest("GET", "http://example.com/vault?list-type=2", nil)
	req.Host = "example.com"
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")

	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalHeaders := "host:example.com\nx-amz-content-sha256:UNSIGNED-PAYLOAD\nx-amz-date:" + amzDate + "\n"
	canonicalQueryString := sortQueryString(req.URL.Query().Encode())

	canonicalRequest := "GET\n/vault\n" + canonicalQueryString + "\n" + canonicalHeaders + "\n" + signedHeaders + "\nUNSIGNED-PAYLOAD"

	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + dateStamp + "/" + region + "/s3/aws4_request\n" + hashSHA256([]byte(canonicalRequest))

	signingKey := deriveSigningKey(secretKey, dateStamp, region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + signature
	req.Header.Set("Authorization", authHeader)

	if !sigV4Verify(req, accessKey, secretKey, region) {
		t.Fatal("expected valid signature to verify")
	}
}

func TestSigV4VerifyTamperedSignature(t *testing.T) {
	accessKey := "AKIAIOSFODNN7EXAMPLE"
	secretKey := "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
	region := "us-east-1"

	req, _ := http.NewRequest("GET", "http://example.com/vault", nil)
	req.Host = "example.com"
	req.Header.Set("X-Amz-Date", "20230101T000000Z")
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/20230101/"+region+"/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=0000000000000000000000000000000000000000000000000000000000000000")

	if sigV4Verify(req, accessKey, secretKey, region) {
		t.Fatal("expected tampered signature to fail")
	}
}
