package pdf

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func tinyPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestQRImageBytes(t *testing.T) {
	pngData := tinyPNG()
	if got := QRImageBytes(""); got != nil {
		t.Errorf("empty = %v", got)
	}
	// Raw PNG bytes pass through.
	if got := QRImageBytes(string(pngData)); !bytes.Equal(got, pngData) {
		t.Errorf("raw png = %v", got)
	}
	// Standard base64 of PNG decodes.
	b64 := base64.StdEncoding.EncodeToString(pngData)
	if got := QRImageBytes(b64); !bytes.Equal(got, pngData) {
		t.Errorf("b64 png mismatch")
	}
	// data-URI prefix handled.
	if got := QRImageBytes("data:image/png;base64," + b64); !bytes.Equal(got, pngData) {
		t.Errorf("data uri mismatch")
	}
	// URL-safe raw base64 handled.
	raw := base64.RawURLEncoding.EncodeToString(pngData)
	if got := QRImageBytes(raw); !bytes.Equal(got, pngData) {
		t.Errorf("url-safe b64 mismatch")
	}
	// URLs and junk are rejected — never fetched.
	for _, bad := range []string{"https://portal.merchantservices.com/qr.png", "not-base64!!", "aGVsbG8=" /* decodes to text */} {
		if got := QRImageBytes(bad); got != nil {
			t.Errorf("QRImageBytes(%q) = %d bytes, want nil", bad, len(got))
		}
	}
}

func TestRsIndianGrouping(t *testing.T) {
	cases := map[float64]string{
		0:          "Rs. 0.00",
		5.5:        "Rs. 5.50",
		1500:       "Rs. 1,500.00",
		250000.75:  "Rs. 2,50,000.75",
		12345678.9: "Rs. 1,23,45,678.90",
		-42.1:      "-Rs. 42.10",
	}
	for in, want := range cases {
		if got := rs(in); got != want {
			t.Errorf("rs(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestClip(t *testing.T) {
	long := strings.Repeat("x", 80)
	if got := clip(long, 60); len([]rune(got)) != 60 || !strings.HasSuffix(got, "…") {
		t.Errorf("clip long = len %d suffix ok=%v", len(got), strings.HasSuffix(got, "…"))
	}
	if got := clip("short", 60); got != "short" {
		t.Errorf("clip short = %q", got)
	}
}
