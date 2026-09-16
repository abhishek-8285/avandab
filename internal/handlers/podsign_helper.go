package handlers

// podSignedURL rewrites a stored POD/signature URL into a short-lived signed
// URL when a signer is wired. Anonymous viewers can then only fetch the asset
// via a URL the application issued, and a leaked link expires (audit
// 2026-09-16). Nil-safe: no signer (tests, legacy wiring) returns the raw URL.
func (a *App) podSignedURL(rawURL string) string {
	if a == nil || a.podSignURL == nil {
		return rawURL
	}
	return a.podSignURL(rawURL)
}
