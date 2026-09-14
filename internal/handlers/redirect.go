package handlers

import (
	"net/http"
	"net/url"
	"strings"
)

// safeRedirect returns a same-origin redirect target derived from target, or
// fallback when target is empty, unparseable, or points at another host.
//
// Redirecting to an attacker-controlled URL is an open redirect, which is a
// phishing vector: a malicious page can link into this app so the Referer
// header carries the attacker's host, and the handler would then bounce the
// just-authenticated user straight off-site. This helper only ever returns a
// path, so the browser can never be sent to another origin.
func safeRedirect(r *http.Request, target, fallback string) string {
	if target == "" {
		return fallback
	}

	// Protocol-relative URLs ("//evil.com", "/\evil.com") inherit the scheme
	// and are treated as absolute by browsers, bypassing a naive
	// "starts with /" check.
	if strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
		return fallback
	}

	u, err := url.Parse(target)
	if err != nil {
		return fallback
	}

	out := target
	if u.Host != "" {
		// Absolute URL: allowed only when it points at this host, and then
		// reduced to its path so the result is provably same-origin.
		if !strings.EqualFold(u.Host, r.Host) {
			return fallback
		}
		out = u.Path
		if u.RawQuery != "" {
			out += "?" + u.RawQuery
		}
	}

	if out == "" || out[0] != '/' {
		return fallback
	}
	return out
}
