package omt

import (
	"bytes"
	"image"
	"testing"

	"github.com/MikanseiLaboratory/vmx-go"
)

// uyvyGradient builds a packed UYVY frame with enough variation to make a
// wrong plane order or matrix visible.
func uyvyGradient(w, h int) []byte {
	src := make([]byte, w*2*h)
	for y := 0; y < h; y++ {
		row := src[y*w*2:]
		for x := 0; x < w; x += 2 {
			i := x * 2
			row[i] = byte(64 + (x*3+y)%128)         // U
			row[i+1] = byte((16 + x*5 + y*7) % 256) // Y0
			row[i+2] = byte(64 + (x*2+y*5)%128)     // V
			row[i+3] = byte((16 + (x+1)*5 + y*7) % 256)
		}
	}
	return src
}

func encodeVMX1(t *testing.T, w, h int, cs ColorSpace) []byte {
	t.Helper()
	enc, err := vmx.New(vmx.Config{Width: w, Height: h, Profile: vmx.ProfileOmtSQ, ColorSpace: vmx.ColorSpace(cs)})
	if err != nil {
		t.Fatalf("vmx.New(%dx%d): %v", w, h, err)
	}
	if err := enc.EncodeUYVY(uyvyGradient(w, h), w*2); err != nil {
		t.Fatalf("encode %dx%d: %v", w, h, err)
	}
	packet := make([]byte, enc.MaxEncodedSize())
	n, err := enc.SaveTo(packet)
	if err != nil {
		t.Fatalf("save %dx%d: %v", w, h, err)
	}
	return packet[:n]
}

// referenceRGBA decodes data with a fresh vmx.Codec, mirroring what
// VideoDecoder is expected to produce.
func referenceRGBA(t *testing.T, w, h int, cs ColorSpace, data []byte) *image.RGBA {
	t.Helper()
	dec, err := vmx.New(vmx.Config{Width: w, Height: h, Profile: vmx.ProfileOmtSQ, ColorSpace: vmx.ColorSpace(cs)})
	if err != nil {
		t.Fatalf("vmx.New(%dx%d): %v", w, h, err)
	}
	if err := dec.LoadFrom(data); err != nil {
		t.Fatalf("load: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := dec.DecodeRGBA(img.Pix, img.Stride); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return img
}

func videoFrame(t *testing.T, w, h int, cs ColorSpace) VideoFrame {
	t.Helper()
	return VideoFrame{
		Width: w, Height: h,
		FrameRateN: 30, FrameRateD: 1,
		ColorSpace: cs,
		Data:       encodeVMX1(t, w, h, cs),
	}
}

func TestVideoFrameRGBA(t *testing.T) {
	const w, h = 64, 32
	f := videoFrame(t, w, h, ColorBT709)
	img := f.RGBA()
	if img == nil {
		t.Fatal("RGBA returned nil for a valid frame")
	}
	if got, want := img.Bounds(), image.Rect(0, 0, w, h); got != want {
		t.Fatalf("bounds %v want %v", got, want)
	}
	if want := referenceRGBA(t, w, h, f.ColorSpace, f.Data); !bytes.Equal(img.Pix, want.Pix) {
		t.Fatal("RGBA does not match a direct vmx decode")
	}
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 255 {
			t.Fatalf("pixel %d has alpha %d, want 255", i/4, img.Pix[i])
		}
	}
	if bytes.Equal(img.Pix[:4], img.Pix[len(img.Pix)-4:]) {
		t.Fatal("decoded frame is uniform; the test pattern is broken")
	}
}

func TestVideoFrameRGBAMismatchedColorSpace(t *testing.T) {
	f := videoFrame(t, 64, 32, ColorBT601)
	want := referenceRGBA(t, f.Width, f.Height, ColorBT601, f.Data)
	got := f.RGBA()
	if got == nil || !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("RGBA ignored the frame color space")
	}
}

func TestVideoFrameRGBAErrors(t *testing.T) {
	valid := encodeVMX1(t, 64, 32, ColorBT709)
	cases := []struct {
		name string
		f    VideoFrame
	}{
		{"no payload", VideoFrame{Width: 64, Height: 32}},
		{"zero width", VideoFrame{Width: 0, Height: 32, Data: valid}},
		{"zero height", VideoFrame{Width: 64, Height: 0, Data: valid}},
		{"odd width", VideoFrame{Width: 63, Height: 32, Data: valid}},
		{"garbage payload", VideoFrame{Width: 64, Height: 32, Data: []byte{0, 1, 2, 3}}},
		{"truncated payload", VideoFrame{Width: 64, Height: 32, Data: valid[:len(valid)/2]}},
		{"dimension mismatch", VideoFrame{Width: 64, Height: 64, Data: valid}},
	}
	for _, tc := range cases {
		if img := tc.f.RGBA(); img != nil {
			t.Errorf("%s: RGBA returned an image, want nil", tc.name)
		}
		if _, err := new(VideoDecoder).Decode(tc.f); err == nil {
			t.Errorf("%s: Decode returned no error", tc.name)
		}
	}
}

func TestVideoDecoderReuse(t *testing.T) {
	frames := []VideoFrame{
		videoFrame(t, 64, 32, ColorBT709),
		videoFrame(t, 128, 64, ColorBT601), // new size and color space
		videoFrame(t, 64, 32, ColorBT601),  // same size, different color space
		videoFrame(t, 64, 32, ColorBT709),  // back to the first configuration
	}
	var dec VideoDecoder
	for i, f := range frames {
		img, err := dec.Decode(f)
		if err != nil {
			t.Fatalf("frame %d (%dx%d): %v", i, f.Width, f.Height, err)
		}
		if got, want := img.Bounds(), image.Rect(0, 0, f.Width, f.Height); got != want {
			t.Fatalf("frame %d: bounds %v want %v", i, got, want)
		}
		if want := referenceRGBA(t, f.Width, f.Height, f.ColorSpace, f.Data); !bytes.Equal(img.Pix, want.Pix) {
			t.Fatalf("frame %d: decoded pixels differ from a direct vmx decode", i)
		}
	}
}

func TestVideoDecoderReturnsError(t *testing.T) {
	var dec VideoDecoder
	if _, err := dec.Decode(VideoFrame{Width: 64, Height: 64, Data: []byte{1, 2, 3}}); err == nil {
		t.Fatal("Decode accepted an invalid bitstream")
	}
	// A decode failure must not poison the decoder.
	if _, err := dec.Decode(videoFrame(t, 64, 32, ColorBT709)); err != nil {
		t.Fatalf("Decode after failure: %v", err)
	}
}

func TestVideoDecoderColorSimdPath(t *testing.T) {
	var dec VideoDecoder
	if got := dec.ColorSimdPath(); got != "" {
		t.Fatalf("ColorSimdPath before Decode = %q, want empty", got)
	}
	if _, err := dec.Decode(videoFrame(t, 64, 32, ColorBT709)); err != nil {
		t.Fatal(err)
	}
	if got := dec.ColorSimdPath(); got == "" {
		t.Fatal("ColorSimdPath empty after a successful Decode")
	}
}
