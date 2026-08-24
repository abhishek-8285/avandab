package pdf

import (
	"bytes"
	"encoding/base64"
	"image"
	"strings"

	// Format registration for image.DecodeConfig validation.
	_ "image/jpeg"
	_ "image/png"
)

// QRImageBytes extracts embeddable image bytes from an e-invoice
// signed_qr payload. Accepts raw PNG/JPEG bytes, standard or URL-safe
// base64, and optional data-URI prefixes. Returns nil for anything else
// (URLs are NEVER fetched — no network calls from the PDF path).
// Returned bytes are validated decodable images so the PDF generator
// never sees corrupt input (fpdf panics on short buffers).
func QRImageBytes(signedQR string) []byte {
	s := strings.TrimSpace(signedQR)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "data:image") {
		if i := strings.Index(s, ","); i >= 0 {
			s = s[i+1:]
		}
	}
	for _, candidate := range [][]byte{[]byte(s), decodeBase64(s)} {
		if len(candidate) == 0 {
			continue
		}
		if isPNG(candidate) || isJPEG(candidate) {
			if _, _, err := image.DecodeConfig(bytes.NewReader(candidate)); err == nil {
				return candidate
			}
		}
	}
	return nil
}

func decodeBase64(s string) []byte {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b
		}
	}
	return nil
}
