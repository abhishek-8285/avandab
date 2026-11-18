package handlers

import "strings"

// outOfBandURL builds an absolute link for email delivery. The origin comes
// from configuration, never from r.Host or X-Forwarded-Proto: those are
// attacker-controlled on an unauthenticated forgot-password request and were
// used to send victims' reset tokens to an attacker host.
func (h *AuthHandlers) outOfBandURL(path string) string {
	base := ""
	if h.App != nil && h.App.Config != nil {
		base = strings.TrimRight(h.Config.PublicBaseURL, "/")
	}
	if base == "" {
		base = "https://avandab.com" // safe default, matches config default
	}
	return base + path
}
