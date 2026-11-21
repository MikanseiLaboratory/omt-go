// Command gio-monitor is a small OMT monitor and test-pattern sender.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	omt "github.com/MikanseiLaboratory/omt-go"
	"github.com/MikanseiLaboratory/vmx-go"
)

type mode string

const (
	modeMonitor mode = "monitor"
	modeSend    mode = "send"
)

type frame struct {
	img      *image.RGBA
	width    int
	height   int
	fps      float64
	decodeMS float64
	frames   uint64
	drops    uint64
	info     string
}

type sourceProvider interface {
	Refresh(time.Duration) []omt.Source
	Close()
}

type appState struct {
	win   *app.Window
	theme *material.Theme
	mode  mode

	mu      sync.Mutex
	status  string
	sources []omt.Source
	srcBtns []widget.Clickable

	refresh widget.Clickable
	connect widget.Clickable
	editor  widget.Editor

	browser sourceProvider
	rx      *omt.Receiver

	sender *omt.Sender
	codec  *vmx.Codec
	latest atomic.Pointer[frame]

	name            string
	fps             int
	width           int
	height          int
	port            int
	discoveryServer string

	stop chan struct{}
}

func main() {
	m := flag.String("mode", "monitor", "monitor or send")
	name := flag.String("name", "Go Test Patterns", "sender source name")
	url := flag.String("url", "", "initial omt:// URL")
	fps := flag.Int("fps", 30, "sender frame rate")
	width := flag.Int("width", 1920, "sender width")
	height := flag.Int("height", 1080, "sender height")
	port := flag.Int("port", 0, "sender port (0 = 6400-6600)")
	ds := flag.String("discovery-server", "", "OMT discovery server URL (empty = DNS-SD)")
	flag.Parse()

	a := &appState{
		mode: mode(*m), name: *name, fps: *fps, width: *width, height: *height, port: *port,
		discoveryServer: *ds, stop: make(chan struct{}),
	}
	if a.mode != modeMonitor && a.mode != modeSend {
		log.Fatalf("unknown mode %q", *m)
	}
	go func() {
		if err := a.run(*url); err != nil {
			log.Fatal(err)
		}
	}()
	app.Main()
}

func (a *appState) run(initialURL string) error {
	a.win = new(app.Window)
	a.win.Option(app.Title("OMT Go Monitor"), app.Size(unit.Dp(1280), unit.Dp(760)))
	a.theme = material.NewTheme()
	if a.mode == modeMonitor {
		if a.discoveryServer != "" {
			b, err := omt.NewServerBrowser(a.discoveryServer)
			if err != nil {
				return err
			}
			a.browser = b
		} else {
			a.browser = omt.NewBrowser()
		}
		a.editor.SingleLine = true
		if initialURL != "" {
			a.editor.SetText(initialURL)
		}
		go func() {
			a.refreshSources()
			if initialURL != "" {
				a.connectURL(initialURL)
			}
		}()
	} else {
		if err := a.startSender(); err != nil {
			a.setStatus("send error: " + err.Error())
		}
	}
	var ops op.Ops
	for {
		e := a.win.Event()
		switch e := e.(type) {
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		case app.DestroyEvent:
			a.close()
			return e.Err
		}
	}
}

func (a *appState) close() {
	select {
	case <-a.stop:
	default:
		close(a.stop)
	}
	if a.rx != nil {
		a.rx.Close()
	}
	if a.browser != nil {
		a.browser.Close()
	}
	if a.sender != nil {
		a.sender.Close()
	}
}

func (a *appState) setStatus(s string) {
	a.mu.Lock()
	a.status = s
	a.mu.Unlock()
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *appState) getStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *appState) refreshSources() {
	srcs := a.browser.Refresh(1500 * time.Millisecond)
	a.mu.Lock()
	a.sources = srcs
	a.mu.Unlock()
	a.setStatus(fmt.Sprintf("%d source(s)", len(srcs)))
	a.win.Invalidate()
}

func (a *appState) layout(gtx layout.Context) layout.Dimensions {
	paint.Fill(gtx.Ops, color.NRGBA{R: 16, G: 18, B: 22, A: 255})
	if a.mode == modeMonitor {
		return a.layoutMonitor(gtx)
	}
	return a.layoutSender(gtx)
}

func (a *appState) topBar(gtx layout.Context, title string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(a.theme, unit.Sp(20), title)
				l.Color = color.NRGBA{R: 240, G: 240, B: 240, A: 255}
				return l.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 1)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(a.theme, unit.Sp(12), a.getStatus())
				l.Color = color.NRGBA{R: 160, G: 170, B: 180, A: 255}
				return l.Layout(gtx)
			}),
		)
	})
}

func (a *appState) layoutMonitor(gtx layout.Context) layout.Dimensions {
	a.mu.Lock()
	srcs := append([]omt.Source(nil), a.sources...)
	a.mu.Unlock()
	if len(a.srcBtns) < len(srcs) {
		a.srcBtns = append(a.srcBtns, make([]widget.Clickable, len(srcs)-len(a.srcBtns))...)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.topBar(gtx, "OMT Go Monitor") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(unit.Dp(8))}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return material.Editor(a.theme, &a.editor, "omt://host:port/source").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if a.connect.Clicked(gtx) {
							url := a.editor.Text()
							if url == "" && len(srcs) > 0 {
								url = srcs[0].URL
							}
							if url != "" {
								a.connectURL(url)
							}
						}
						return material.Button(a.theme, &a.connect, "Connect").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if a.refresh.Clicked(gtx) {
							go a.refreshSources()
						}
						return material.Button(a.theme, &a.refresh, "Refresh").Layout(gtx)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(srcs) == 0 {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(unit.Dp(6))}.Layout(gtx, func() []layout.FlexChild {
					children := make([]layout.FlexChild, 0, len(srcs))
					for i := range srcs {
						i := i
						if i >= 6 {
							break
						}
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if a.srcBtns[i].Clicked(gtx) {
								a.connectURL(srcs[i].URL)
							}
							b := material.Button(a.theme, &a.srcBtns[i], srcs[i].Name)
							return b.Layout(gtx)
						}))
					}
					return children
				}()...)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.drawVideo(gtx, a.latest.Load())
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Top: unit.Dp(6), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return a.drawStats(gtx, a.latest.Load())
			})
		}),
	)
}

func (a *appState) layoutSender(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.topBar(gtx, "OMT Go Test Patterns") }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return a.drawVideo(gtx, a.latest.Load()) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Top: unit.Dp(6), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return a.drawStats(gtx, a.latest.Load())
			})
		}),
	)
}

func (a *appState) drawVideo(gtx layout.Context, f *frame) layout.Dimensions {
	sz := gtx.Constraints.Max
	if f == nil || f.img == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.Label(a.theme, unit.Sp(16), "No video").Layout(gtx)
		})
	}
	iw, ih := f.img.Rect.Dx(), f.img.Rect.Dy()
	if iw <= 0 || ih <= 0 {
		return layout.Dimensions{Size: sz}
	}
	scale := math.Min(float64(sz.X)/float64(iw), float64(sz.Y)/float64(ih))
	w, h := int(float64(iw)*scale), int(float64(ih)*scale)
	off := image.Pt((sz.X-w)/2, (sz.Y-h)/2)
	defer op.Offset(off).Push(gtx.Ops).Pop()
	defer clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops).Pop()
	op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(scale), float32(scale)))).Add(gtx.Ops)
	paint.NewImageOp(f.img).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: sz}
}

func (a *appState) drawStats(gtx layout.Context, f *frame) layout.Dimensions {
	if f == nil {
		return material.Label(a.theme, unit.Sp(12), "waiting for video...").Layout(gtx)
	}
	text := fmt.Sprintf("%dx%d  %.2f fps  decode %.2f ms  frames %d  drops %d  %s",
		f.width, f.height, f.fps, f.decodeMS, f.frames, f.drops, f.info)
	l := material.Label(a.theme, unit.Sp(12), text)
	l.Color = color.NRGBA{R: 180, G: 190, B: 200, A: 255}
	return l.Layout(gtx)
}

func (a *appState) connectURL(url string) {
	if a.rx != nil {
		a.rx.Close()
		a.rx = nil
	}
	rx, err := omt.Dial(url)
	if err != nil {
		a.setStatus("connect error: " + err.Error())
		return
	}
	a.rx = rx
	a.setStatus("connected: " + url)
	go a.decodeLoop(rx)
}

func (a *appState) decodeLoop(rx *omt.Receiver) {
	log.Printf("decode loop started")
	var codec *vmx.Codec
	var last time.Time
	var fps float64
	var count uint64
	for {
		select {
		case <-a.stop:
			return
		case v, ok := <-rx.Videos():
			if !ok {
				return
			}
			if v.Width <= 0 || v.Height <= 0 {
				continue
			}
			if codec == nil {
				c, err := vmx.New(vmx.Config{Width: v.Width, Height: v.Height, Profile: vmx.ProfileOmtSQ, ColorSpace: vmx.ColorSpace(v.ColorSpace)})
				if err != nil {
					a.setStatus("codec: " + err.Error())
					return
				}
				codec = c
			}
			start := time.Now()
			if err := codec.LoadFrom(v.Data); err != nil {
				log.Printf("load error: %v", err)
				a.setStatus("decode: " + err.Error())
				continue
			}
			img := image.NewRGBA(image.Rect(0, 0, v.Width, v.Height))
			if err := codec.DecodeRGBA(img.Pix, img.Stride); err != nil {
				log.Printf("decode error: %v", err)
				a.setStatus("decode: " + err.Error())
				continue
			}
			elapsed := time.Since(start)
			now := time.Now()
			if !last.IsZero() && now.Sub(last) > 0 {
				inst := 1 / now.Sub(last).Seconds()
				if fps == 0 {
					fps = inst
				} else {
					fps = fps*0.9 + inst*0.1
				}
			}
			last = now
			count++
			a.latest.Store(&frame{img: img, width: v.Width, height: v.Height, fps: fps,
				decodeMS: float64(elapsed.Microseconds()) / 1000, frames: count, drops: rx.Stats().FramesDropped})
			a.win.Invalidate()
		}
	}
}

func (a *appState) startSender() error {
	opts := []omt.Option{omt.WithPort(a.port), omt.WithInfo(omt.SenderInfo{
		ProductName: "omt-go Test Patterns", Manufacturer: "MikanseiLaboratory", Version: "0.1.0",
	})}
	if a.discoveryServer != "" {
		opts = append(opts, omt.WithDiscoveryServer(a.discoveryServer))
	}
	s, err := omt.NewSender(a.name, opts...)
	if err != nil {
		return err
	}
	a.sender = s
	if a.fps < 1 {
		a.fps = 30
	}
	c, err := vmx.New(vmx.Config{Width: a.width, Height: a.height, Profile: vmx.ProfileOmtSQ, ColorSpace: vmx.ColorBT709})
	if err != nil {
		s.Close()
		return err
	}
	a.codec = c
	a.setStatus(fmt.Sprintf("sending %s on port %d", a.name, s.Port()))
	go a.sendLoop()
	go a.audioLoop()
	return nil
}

func (a *appState) sendLoop() {
	ticker := time.NewTicker(time.Second / time.Duration(a.fps))
	defer ticker.Stop()
	var idx uint64
	start := time.Now()
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
		}
		uyvy, rgba, err := makePattern(a.width, a.height, int(idx))
		if err != nil {
			a.setStatus("pattern: " + err.Error())
			return
		}
		if err := a.codec.EncodeUYVY(uyvy, a.width*2); err != nil {
			a.setStatus("encode: " + err.Error())
			continue
		}
		packet := make([]byte, a.codec.MaxEncodedSize())
		n, err := a.codec.SaveTo(packet)
		if err != nil {
			a.setStatus("encode: " + err.Error())
			continue
		}
		ts := int64(float64(idx) * 10_000_000 / float64(a.fps))
		if err := a.sender.SendVideo(omt.VideoFrame{Width: a.width, Height: a.height, FrameRateN: a.fps, FrameRateD: 1,
			Aspect: float32(a.width) / float32(a.height), ColorSpace: omt.ColorBT709, Timestamp: ts, Data: packet[:n]}); err != nil {
			a.setStatus("send: " + err.Error())
		}
		elapsed := time.Since(start)
		fps := float64(idx+1) / elapsed.Seconds()
		a.latest.Store(&frame{img: rgba, width: a.width, height: a.height, fps: fps,
			frames: idx + 1, info: fmt.Sprintf("port %d  subscribers %d", a.sender.Port(), a.sender.Stats().VideoSubscribers)})
		a.win.Invalidate()
		idx++
	}
}

func (a *appState) audioLoop() {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var phase float64
	var samples int64
	const rate = 48000
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
		}
		n := 960
		buf := make([]byte, n*2*4)
		for i := 0; i < n; i++ {
			v := float32(0.1 * math.Sin(phase))
			phase += 2 * math.Pi * 1000 / rate
			if phase > 2*math.Pi {
				phase -= 2 * math.Pi
			}
			binaryPutFloat32(buf[i*4:], v)
			binaryPutFloat32(buf[(n+i)*4:], v)
		}
		_ = a.sender.SendAudio(omt.AudioFrame{SampleRate: rate, SamplesPerChannel: n, Channels: 2,
			ActiveChannels: 3, Timestamp: int64(float64(samples) * 10_000_000 / rate), Data: buf})
		samples += int64(n)
	}
}

func binaryPutFloat32(b []byte, v float32) {
	u := math.Float32bits(v)
	b[0] = byte(u)
	b[1] = byte(u >> 8)
	b[2] = byte(u >> 16)
	b[3] = byte(u >> 24)
}
