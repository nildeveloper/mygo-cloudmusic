package systemplayer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// whiteIconPNG keeps the glyph's alpha mask, including antialiased edges, but
// bakes in white so Touch Bar does not render the menu bar's black template ink.
func whiteIconPNG(data []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	result := image.NewNRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			_, _, _, alpha := src.At(x, y).RGBA()
			result.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(alpha >> 8)})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, result); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
