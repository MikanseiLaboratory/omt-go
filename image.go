package omt

import (
	"fmt"
	"image"

	"github.com/MikanseiLaboratory/vmx-go"
)

// FrameDecoder converts encoded video frames into RGBA images.
type FrameDecoder interface {
	Decode(VideoFrame) (*image.RGBA, error)
}

// VideoDecoder converts VMX1 video frames into image.RGBA while reusing its
// codec and pixel buffer. The zero value is ready to use. A VideoDecoder is
// not safe for concurrent use; create one per goroutine.
type VideoDecoder struct {
	codec      *vmx.Codec
	width      int
	height     int
	colorSpace ColorSpace
	img        *image.RGBA
}

var _ FrameDecoder = (*VideoDecoder)(nil)

// Decode converts f into an RGBA image.
//
// f must carry a self-contained VMX1 bitstream, as sent by SendVideo, together
// with the Width, Height and ColorSpace that describe it. The returned image
// is reused by later calls to Decode and stays valid only until the next call.
//
// VMX1 alpha, 1/8 preview and high bit depth frames are not supported.
func (d *VideoDecoder) Decode(f VideoFrame) (*image.RGBA, error) {
	if f.Width <= 0 || f.Height <= 0 {
		return nil, fmt.Errorf("omt: invalid video dimensions %dx%d", f.Width, f.Height)
	}
	if len(f.Data) == 0 {
		return nil, fmt.Errorf("omt: video frame has no payload")
	}
	if d.img == nil || d.width != f.Width || d.height != f.Height || d.colorSpace != f.ColorSpace {
		c, err := vmx.New(vmx.Config{
			Width:      f.Width,
			Height:     f.Height,
			Profile:    vmx.ProfileOmtSQ,
			ColorSpace: vmx.ColorSpace(f.ColorSpace),
		})
		if err != nil {
			return nil, err
		}
		d.codec = c
		d.width, d.height, d.colorSpace = f.Width, f.Height, f.ColorSpace
		d.img = image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	}
	if err := d.codec.LoadFrom(f.Data); err != nil {
		return nil, fmt.Errorf("omt: load VMX1 frame: %w", err)
	}
	if err := d.codec.DecodeRGBA(d.img.Pix, d.img.Stride); err != nil {
		return nil, fmt.Errorf("omt: decode VMX1 frame: %w", err)
	}
	return d.img, nil
}

// ColorSimdPath reports the colour conversion kernel of the current codec
// ("avx2", "neon" or "scalar"), or "" before the first Decode.
func (d *VideoDecoder) ColorSimdPath() string {
	if d.codec == nil {
		return ""
	}
	return d.codec.ColorSimdPath()
}

// RGBA decodes the frame into a new image.RGBA.
//
// The frame must carry a self-contained VMX1 bitstream, as sent by SendVideo,
// with Width, Height and ColorSpace describing it. RGBA returns nil when the
// frame cannot be decoded; use VideoDecoder.Decode when the error matters.
//
// RGBA allocates a codec and pixel buffer per call. To decode a stream of
// frames, reuse a VideoDecoder instead.
func (f VideoFrame) RGBA() *image.RGBA {
	var d VideoDecoder
	img, err := d.Decode(f)
	if err != nil {
		return nil
	}
	return img
}
