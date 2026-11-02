package oauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			img.Set(x, y, color.NRGBA{R: 200, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func decodeLogo(t *testing.T, dataURL string) image.Image {
	t.Helper()
	raw, ok := strings.CutPrefix(dataURL, "data:image/png;base64,")
	if !ok {
		t.Fatalf("logo = %.40q", dataURL)
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestLogosAreRedrawnSquare(t *testing.T) {
	img := decodeLogo(t, redrawLogo(encodePNG(t, 400, 200)))
	if img.Bounds().Dx() != logoSize || img.Bounds().Dy() != logoSize {
		t.Fatalf("size = %v", img.Bounds())
	}
	// A wide logo is centered, with transparency above and below it.
	if _, _, _, a := img.At(logoSize/2, 2).RGBA(); a != 0 {
		t.Error("the space above a wide logo isn't transparent")
	}
	if r, _, _, a := img.At(logoSize/2, logoSize/2).RGBA(); a == 0 || r == 0 {
		t.Error("the logo itself is missing")
	}

	for name, data := range map[string][]byte{
		"not an image": []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"),
		"empty":        nil,
		// A tiny file can claim a huge image; its header is read first.
		"too many pixels": func() []byte {
			b := encodePNG(t, 1, 1)
			// The header's width and height sit at bytes 16 to 23, and its
			// checksum, over its type and data, after them.
			binary.BigEndian.PutUint32(b[16:], 1<<16)
			binary.BigEndian.PutUint32(b[20:], 1<<16)
			binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
			if config, err := png.DecodeConfig(bytes.NewReader(b)); err != nil || config.Width != 1<<16 {
				t.Fatalf("the huge header doesn't read: %v", err)
			}
			return b
		}(),
	} {
		if got := redrawLogo(data); got != "" {
			t.Errorf("%s was drawn", name)
		}
	}
}

func TestLogosAreFetchedLikeMetadata(t *testing.T) {
	logo := encodePNG(t, 64, 64)
	s, logoURL := metadataServer(t, func(w http.ResponseWriter, _ string) { _, _ = w.Write(logo) })
	if got := s.fetchLogo(context.Background(), logoURL); got == "" {
		t.Error("a PNG logo wasn't fetched")
	}
	for _, raw := range []string{"", "http://example.com/logo.png", "javascript:alert(1)", "https://user@example.com/logo.png"} {
		if got := s.fetchLogo(context.Background(), raw); got != "" {
			t.Errorf("%q was fetched", raw)
		}
	}

	missing, missingURL := metadataServer(t, func(w http.ResponseWriter, _ string) { w.WriteHeader(http.StatusNotFound) })
	if got := missing.fetchLogo(context.Background(), missingURL); got != "" {
		t.Error("a missing logo was drawn")
	}
	big, bigURL := metadataServer(t, func(w http.ResponseWriter, _ string) {
		_, _ = w.Write(append(encodePNG(t, 8, 8), make([]byte, maxLogoBytes)...))
	})
	if got := big.fetchLogo(context.Background(), bigURL); got != "" {
		t.Error("a logo over the size limit was drawn")
	}

	// The real client refuses this machine, for logos as for documents.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(logo) }))
	defer srv.Close()
	if got := NewServer(base, "Acme").fetchLogo(context.Background(), srv.URL+"/logo.png"); got != "" {
		t.Error("a logo was fetched from this machine")
	}
}
