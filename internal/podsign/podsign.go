// Package podsign provides short-lived signed URLs for proof-of-delivery
// (POD) assets served under /uploads/pod/*.
//
// Threat model (see audit 2026-09-16):
//
//	Until now /uploads/pod/* was mounted WITHOUT authentication so the public
//	ePOD certificate page (epod_receipt.html) could render POD photos to an
//	anonymous consignee. Because filenames are random UUIDs, guessing is not
//	practical, but anyone who learns or leaks a URL (browser history, shared
//	screenshot, referrer header to a third-party analytics endpoint) could
//	read that POD forever, with no audit trail.
//
// Fix: every POD URL we hand out is signed with an HMAC over
// (filename, expiry). The public mount only serves when the signature
// verifies AND the expiry has not passed. Anonymous callers therefore get
// time-boxed access only via URLs the application itself issued, and can
// no longer reuse a leaked URL indefinitely.
package podsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaxTTL bounds how long a signed POD URL stays usable. Public e-POD pages
// are opened by consignees right after delivery, so a short window keeps
// the page usable while bounding exposure of a leaked link.
const MaxTTL = 24 * time.Hour

// DefaultTTL is used when a caller asks for a signed URL without stating a
// lifetime; it is the default validity window for public e-POD links.
const DefaultTTL = 6 * time.Hour

// minTTL guards against accidentally signing URLs that are already expired
// at issue time.
const minTTL = time.Second

var (
	// ErrInvalidSignature is returned when a signed URL's signature does not
	// match the recomputed HMAC, or when required query parameters are absent.
	ErrInvalidSignature = errors.New("podsign: invalid signature")
	// ErrExpired is returned when the signature is valid but the expiry has passed.
	ErrExpired = errors.New("podsign: signature expired")
)

// Signer issues and verifies signed POD URLs using an HMAC-SHA256 key.
// The key must come from process configuration (SESSION_TOKEN_SECRET /
// POD_SIGN_SECRET), never from code.
type Signer struct {
	secret []byte
	ttl    time.Duration
	clock  func() time.Time
}

// New returns a Signer using the given key. An empty key is rejected so a
// misconfigured deployment fails closed instead of signing with a zero key.
func New(secret []byte, ttl time.Duration) (*Signer, error) {
	if len(secret) < 16 {
		return nil, errors.New("podsign: secret must be at least 16 bytes")
	}
	if ttl < minTTL || ttl > MaxTTL {
		ttl = DefaultTTL
	}
	return &Signer{
		secret: append([]byte(nil), secret...),
		ttl:    ttl,
		clock:  time.Now,
	}, nil
}

// Sign produces a signed /uploads/pod/<filename>?exp=<unix>&sig=<hex> URL.
// The input may be a bare filename or a full existing /uploads/pod/<file>
// path; only the filename is signed.
func (s *Signer) Sign(podURL string) (string, error) {
	if s == nil || len(s.secret) == 0 {
		return "", ErrInvalidSignature
	}
	filename := extractFilename(podURL)
	if filename == "" {
		return "", fmt.Errorf("podsign: empty filename in %q", podURL)
	}

	exp := s.clock().Add(s.ttl).Unix()
	sig := s.signature(filename, exp)

	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", sig)
	return "/uploads/pod/" + filename + "?" + q.Encode(), nil
}

// Verify checks a request path+query against the signing key. It returns the
// validated filename on success so callers can serve exactly that file and
// nothing else.
func (s *Signer) Verify(path, query string) (string, error) {
	if s == nil || len(s.secret) == 0 {
		return "", ErrInvalidSignature
	}

	filename := extractFilename(path)
	if filename == "" {
		return "", ErrInvalidSignature
	}

	vals, err := url.ParseQuery(query)
	if err != nil {
		return "", ErrInvalidSignature
	}
	expStr := vals.Get("exp")
	sig := vals.Get("sig")
	if expStr == "" || sig == "" {
		return "", ErrInvalidSignature
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return "", ErrInvalidSignature
	}
	if !hmac.Equal([]byte(sig), []byte(s.signature(filename, exp))) {
		return "", ErrInvalidSignature
	}
	if s.clock().Unix() > exp {
		return "", ErrExpired
	}
	return filename, nil
}

// signature computes the hex HMAC-SHA256 over "filename:expiry".
func (s *Signer) signature(filename string, exp int64) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(filename))
	_, _ = mac.Write([]byte{':'})
	_, _ = mac.Write([]byte(strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// extractFilename returns the final path element of a /uploads/pod/ URL or
// bare filename. It strips any query string and rejects traversal attempts.
func extractFilename(p string) string {
	if p == "" {
		return ""
	}
	p = strings.TrimSpace(p)
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimPrefix(p, "/uploads/pod/")
	// filename must be a single element — no directories, no traversal.
	if strings.ContainsAny(p, `/\`) || strings.Contains(p, "..") {
		return ""
	}
	if p == "." || p == "" {
		return ""
	}
	return p
}
