package bedrock

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// signRequest adds AWS Signature V4 headers to an HTTP request.
func signRequest(req *http.Request, accessKey, secretKey, sessionToken, region, service string, payload []byte) {
	signRequestAt(req, accessKey, secretKey, sessionToken, region, service, payload, time.Now().UTC())
}

// signRequestAt is the testable version of signRequest that accepts a fixed timestamp.
func signRequestAt(req *http.Request, accessKey, secretKey, sessionToken, region, service string, payload []byte, now time.Time) {
	timestamp := now.Format("20060102T150405Z")
	datestamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", timestamp)
	if sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", sessionToken)
	}

	canonicalURI := canonicalizePath(req.URL.Path)
	canonicalQuery := canonicalizeQuery(req.URL.Query())
	canonicalHeaders, signedHeaders := canonicalizeHeaders(req)
	payloadHash := sha256Hex(payload)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := datestamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + timestamp + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	signingKey := deriveSigningKey(secretKey, datestamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, credentialScope, signedHeaders, signature,
	))
}

// canonicalizePath URI-encodes each path segment per AWS SigV4 rules.
func canonicalizePath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = uriEncode(seg)
	}
	return strings.Join(segments, "/")
}

// uriEncode percent-encodes all characters except unreserved (A-Za-z0-9-_.~).
func uriEncode(s string) string {
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			buf.WriteByte(c)
		} else {
			fmt.Fprintf(&buf, "%%%02X", c)
		}
	}
	return buf.String()
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~'
}

func canonicalizeQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := values[k]
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k)+"="+uriEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

func canonicalizeHeaders(req *http.Request) (canonical, signed string) {
	type hdr struct{ name, value string }
	var hdrs []hdr

	for name, vals := range req.Header {
		hdrs = append(hdrs, hdr{strings.ToLower(name), strings.TrimSpace(vals[0])})
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	hdrs = append(hdrs, hdr{"host", host})

	sort.Slice(hdrs, func(i, j int) bool { return hdrs[i].name < hdrs[j].name })

	// Deduplicate (host may already be in req.Header).
	seen := make(map[string]bool, len(hdrs))
	unique := hdrs[:0]
	for _, h := range hdrs {
		if !seen[h.name] {
			seen[h.name] = true
			unique = append(unique, h)
		}
	}

	var canonBuf, signedBuf strings.Builder
	for i, h := range unique {
		canonBuf.WriteString(h.name + ":" + h.value + "\n")
		if i > 0 {
			signedBuf.WriteByte(';')
		}
		signedBuf.WriteString(h.name)
	}
	return canonBuf.String(), signedBuf.String()
}

func deriveSigningKey(secretKey, datestamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(datestamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
