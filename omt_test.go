package omt

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/MikanseiLaboratory/vmx-go"
)

func TestFrameRoundTrip(t *testing.T) {
	v := VideoFrame{Width: 64, Height: 64, FrameRateN: 30, FrameRateD: 1, Timestamp: 123, Data: []byte{1, 2, 3}, Metadata: "<x/>"}
	b := EncodeVideoFrame(v)
	f, err := ReadFrame(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if f.Video == nil || f.Video.Width != 64 || f.Header.Timestamp != 123 || f.MetadataString() != "<x/>" {
		t.Fatalf("bad frame %+v", f)
	}
}

func TestMetadata(t *testing.T) {
	if s, ok := ParseSubscribe(SubscribeVideoXML); !ok || !s.Video {
		t.Fatal("subscribe")
	}
	st, ok := ParseSettings(`<OMTSettings Preview="false" Quality="High" />`)
	if !ok || st.Preview == nil || *st.Preview || st.Quality == nil || *st.Quality != QualityHigh {
		t.Fatalf("settings %+v", st)
	}
	if tl, ok := ParseTally(TallyProgramXML); !ok || tl.Program != 1 {
		t.Fatal("tally")
	}
	info, ok := ParseSenderInfo(`<OMTInfo ProductName="A" Manufacturer="B" Version="C" />`)
	if !ok || info.ProductName != "A" {
		t.Fatal("info")
	}
}

func TestSenderReceiver(t *testing.T) {
	s, err := NewSender("loop", WithAdvertise(false), WithBind("127.0.0.1"), WithPort(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := Dial(fmt.Sprintf("omt://127.0.0.1:%d/loop", s.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	time.Sleep(150 * time.Millisecond)

	enc, _ := vmx.New(vmx.Config{Width: 64, Height: 64, Profile: vmx.ProfileOmtSQ, ColorSpace: vmx.ColorBT709})
	src := make([]byte, 64*2*64)
	for i := range src {
		src[i] = byte(i * 3)
	}
	if err := enc.EncodeUYVY(src, 64*2); err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, enc.MaxEncodedSize())
	n, err := enc.SaveTo(packet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SendVideo(VideoFrame{Width: 64, Height: 64, FrameRateN: 30, FrameRateD: 1, Data: packet[:n]}); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-r.Videos():
		if len(v.Data) != n {
			t.Fatalf("got %d want %d", len(v.Data), n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDiscoveryServer(t *testing.T) {
	srv, err := ListenDiscoveryServer("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c1, err := NewDiscoveryClient(fmt.Sprintf("omt://127.0.0.1:%d", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	if err := c1.Register(Address{Name: "HOST (Test)", Port: 6500}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	c2, err := NewDiscoveryClient(fmt.Sprintf("omt://127.0.0.1:%d", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(c2.Sources()) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no relayed source")
}
