package systemplayer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// The Touch Bar icon used black template pixels; whitening must preserve coverage at opaque and antialiased edges.
func TestWhiteIconPreservesAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for i, alpha := range []uint8{0, 64, 128, 255} {
		src.SetNRGBA(i, 0, color.NRGBA{A: alpha})
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	data, err := whiteIconPNG(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != src.Bounds() {
		t.Fatal("icon dimensions changed")
	}
	for i, alpha := range []uint8{0, 64, 128, 255} {
		pixel := color.NRGBAModel.Convert(got.At(i, 0)).(color.NRGBA)
		if pixel.A != alpha {
			t.Fatalf("pixel %d coverage changed: %v", i, pixel)
		}
		if alpha > 0 && (pixel.R != 255 || pixel.G != 255 || pixel.B != 255) {
			t.Fatalf("pixel %d is not white: %v", i, pixel)
		}
	}
}
