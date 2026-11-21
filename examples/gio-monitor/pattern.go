package main

import (
	"image"
	"image/color"
)

var bars = [7]color.RGBA{
	{235, 235, 235, 255}, // white
	{235, 235, 16, 255},  // yellow
	{16, 235, 235, 255},  // cyan
	{16, 235, 16, 255},   // green
	{235, 16, 235, 255},  // magenta
	{235, 16, 16, 255},   // red
	{16, 16, 235, 255},   // blue
}

func rgbToYUV(c color.RGBA) (int, int, int) {
	r, g, b := int(c.R), int(c.G), int(c.B)
	y := ((47*r + 157*g + 16*b + 128) >> 8) + 16
	u := ((-26*r - 86*g + 112*b + 128) >> 8) + 128
	v := ((112*r - 102*g - 10*b + 128) >> 8) + 128
	return clamp8(y), clamp8(u), clamp8(v)
}

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// makePattern returns packed UYVY for VMX and an RGBA copy for the local preview.
func makePattern(w, h, frame int) ([]byte, *image.RGBA, error) {
	uyvy := make([]byte, w*2*h)
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	lineX := (frame * 12) % w
	bandTop := h * 3 / 4
	type yuv struct{ y, u, v int }
	barYUV := make([]yuv, len(bars))
	for i, c := range bars {
		y, u, v := rgbToYUV(c)
		barYUV[i] = yuv{y, u, v}
	}
	for y := 0; y < h; y++ {
		row := y * w * 2
		for x := 0; x < w; x += 2 {
			var c0, c1 color.RGBA
			if y >= bandTop {
				g := uint8(x * 255 / maxInt(1, w-1))
				c0 = color.RGBA{g, g, g, 255}
				c1 = color.RGBA{g, g, g, 255}
				if x >= lineX && x < lineX+24 {
					c0 = color.RGBA{255, 255, 255, 255}
					c1 = color.RGBA{255, 255, 255, 255}
				}
			} else {
				bi := x * len(bars) / maxInt(1, w)
				c0 = bars[bi]
				c1 = bars[bi]
				if x == lineX || x+1 == lineX {
					c0 = color.RGBA{255, 255, 255, 255}
					c1 = color.RGBA{255, 255, 255, 255}
				}
			}
			y0, u0, v0 := rgbToYUV(c0)
			y1, u1, v1 := rgbToYUV(c1)
			o := row + x*2
			uyvy[o] = byte((u0 + u1) / 2)
			uyvy[o+1] = byte(y0)
			uyvy[o+2] = byte((v0 + v1) / 2)
			uyvy[o+3] = byte(y1)
			rgba.SetRGBA(x, y, c0)
			rgba.SetRGBA(x+1, y, c1)
		}
	}
	// Frame counter: a row of blocks whose pattern changes with the frame index.
	block := maxInt(8, w/64)
	for i := 0; i < 16; i++ {
		on := (frame>>uint(i%16))&1 == 1
		col := color.RGBA{16, 16, 16, 255}
		if on {
			col = color.RGBA{235, 235, 235, 255}
		}
		x0 := w/8 + i*block*1
		y0 := bandTop + h/16
		for yy := y0; yy < y0+block && yy < h; yy++ {
			for xx := x0; xx < x0+block && xx < w; xx++ {
				rgba.SetRGBA(xx, yy, col)
				o := yy*w*2 + (xx/2)*4
				y, u, v := rgbToYUV(col)
				uyvy[o] = byte(u)
				uyvy[o+1] = byte(y)
				uyvy[o+2] = byte(v)
				uyvy[o+3] = byte(y)
			}
		}
	}
	return uyvy, rgba, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
