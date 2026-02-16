package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
)

func sigV4Verify(r *http.Request, accessKey, secretKey, region string) bool {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	// Parse: AWS4-HMAC-SHA256 Credential=KEY/DATE/REGION/s3/aws4_request, SignedHeaders=..., Signature=...
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		return false
	}

	parts := strings.TrimPrefix(authHeader, "AWS4-HMAC-SHA256 ")
	fields := make(map[string]string)
	for _, part := range strings.Split(parts, ", ") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			fields[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	credential := fields["Credential"]
	signedHeadersStr := fields["SignedHeaders"]
	signature := fields["Signature"]

	if credential == "" || signedHeadersStr == "" || signature == "" {
		return false
	}

	// Parse credential: accessKey/date/region/s3/aws4_request
	credParts := strings.Split(credential, "/")
	if len(credParts) != 5 || credParts[0] != accessKey {
		return false
	}
	dateStamp := credParts[1]
	credRegion := credParts[2]
	service := credParts[3]

	if credRegion != region {
		return false
	}

	// Build canonical request
	signedHeaders := strings.Split(signedHeadersStr, ";")
	sort.Strings(signedHeaders)

	var canonicalHeaders strings.Builder
	for _, h := range signedHeaders {
		var val string
		if h == "host" {
			val = r.Host
		} else {
			val = strings.TrimSpace(r.Header.Get(h))
		}
		canonicalHeaders.WriteString(h + ":" + val + "\n")
	}

	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}

	canonicalURI := r.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalQueryString := r.URL.Query().Encode()
	canonicalQueryString = sortQueryString(canonicalQueryString)

	canonicalRequest := strings.Join([]string{
		r.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders.String(),
		signedHeadersStr,
		payloadHash,
	}, "\n")

	// String to sign
	amzDate := r.Header.Get("X-Amz-Date")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		dateStamp + "/" + credRegion + "/" + service + "/aws4_request",
		hashSHA256([]byte(canonicalRequest)),
	}, "\n")

	// Signing key
	signingKey := deriveSigningKey(secretKey, dateStamp, credRegion, service)

	// Calculate signature
	expectedSig := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

func sortQueryString(qs string) string {
	if qs == "" {
		return ""
	}
	pairs := strings.Split(qs, "&")
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func hashSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}
