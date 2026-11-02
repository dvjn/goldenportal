package oauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	_ "image/gif" // Logos may be GIF, JPEG, PNG or WebP.
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// A metadata document's logo is fetched with it, as the draft suggests,
// then redrawn small: the portal shows its own PNG, never the bytes a
// client's server sent, and keeps it beside the client.
const (
	maxLogoBytes  = 1 << 20
	maxLogoPixels = 4096 * 4096
	logoSize      = 96
)

// fetchLogo returns a client's logo as a data URL, or nothing when there's
// no logo that can be used: a logo is a courtesy, so it never stops
// someone authorizing the app.
func (s Server) fetchLogo(ctx context.Context, raw string) string {
	u, err := url.Parse(raw)
	if raw == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "image/png, image/webp, image/jpeg, image/gif")
	res, err := s.metadata.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxLogoBytes+1))
	if err != nil || len(body) > maxLogoBytes {
		return ""
	}

	return redrawLogo(body)
}

// redrawLogo scales an image to fit a square logo, centered on
// transparency, and encodes it as a PNG data URL. Its size is read before
// it's decoded, so a small file can't claim a huge image.
func redrawLogo(data []byte) string {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxLogoPixels {
		return ""
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	b := src.Bounds()
	width, height := logoSize, logoSize
	if b.Dx() > b.Dy() {
		height = max(1, logoSize*b.Dy()/b.Dx())
	} else {
		width = max(1, logoSize*b.Dx()/b.Dy())
	}
	dst := image.NewNRGBA(image.Rect(0, 0, logoSize, logoSize))
	at := image.Rect((logoSize-width)/2, (logoSize-height)/2, (logoSize+width)/2, (logoSize+height)/2)
	draw.CatmullRom.Scale(dst, at, src, b, draw.Over, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return ""
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}
