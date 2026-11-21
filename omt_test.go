package omt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
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

func rawMetadataFrame(xml string, nul bool) []byte {
	data := []byte(xml)
	if nul {
		data = append(data, 0)
	}
	b := make([]byte, HeaderSize+len(data))
	b[0] = ProtocolVersion
	b[1] = byte(FrameMetadata)
	binary.LittleEndian.PutUint32(b[12:16], uint32(len(data)))
	copy(b[HeaderSize:], data)
	return b
}

func TestFrameXMLNullTerminated(t *testing.T) {
	info := SenderInfo{ProductName: "P", Manufacturer: "M", Version: "V"}
	for _, nul := range []bool{false, true} {
		f, err := ReadFrame(bytes.NewReader(rawMetadataFrame(info.XML(), nul)))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := ParseSenderInfo(f.XML())
		if !ok || got.ProductName != "P" {
			t.Fatalf("nul=%v: XML %q did not parse: %+v ok=%v", nul, f.XML(), got, ok)
		}
	}
	addr := Address{Name: "HOST (Null)", Port: 6400}
	f, err := ReadFrame(bytes.NewReader(rawMetadataFrame(addr.XML(), true)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseAddress(f.XML())
	if err != nil || got.Name != addr.Name || got.Port != addr.Port {
		t.Fatalf("address parse: %+v err=%v", got, err)
	}
}

func TestDialSendsSubscriptions(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 8)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			f, err := ReadFrame(conn)
			if err != nil {
				return
			}
			got <- f.XML()
		}
	}()
	r, err := Dial("omt://" + ln.Addr().String() + "/fake")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	want := []string{SubscribeMetaXML, PreviewOffXML, SubscribeVideoXML, QualityXML(QualityDefault)}
	for _, w := range want {
		select {
		case g := <-got:
			if g != w {
				t.Fatalf("got %q want %q", g, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for %q", w)
		}
	}
}

func TestSenderRejectsOversizedMetadata(t *testing.T) {
	s, err := NewSender("loop", WithAdvertise(false), WithBind("127.0.0.1"), WithPort(0))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	big := strings.Repeat("x", MaxMetadataSize+1)
	if err := s.SendVideo(VideoFrame{Width: 64, Height: 64, Metadata: big}); err == nil {
		t.Fatal("oversized video metadata accepted")
	}
	if err := s.SendAudio(AudioFrame{SampleRate: 48000, SamplesPerChannel: 1, Channels: 1, Metadata: big}); err == nil {
		t.Fatal("oversized audio metadata accepted")
	}
	if err := s.SendMetadata(big); err == nil {
		t.Fatal("oversized metadata frame accepted")
	}
}

func TestSenderDiscoveryFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if _, err := NewSender("loop", WithAdvertise(true), WithPort(0),
		WithDiscoveryServer(fmt.Sprintf("omt://127.0.0.1:%d", port))); err == nil {
		t.Fatal("sender silently accepted an unreachable discovery server")
	}
}

func TestSourceURLIPv6(t *testing.T) {
	if got, want := sourceURL("::1", 6400, nil, "src"), "omt://[::1]:6400/src"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got, want := sourceURL("host", 6400, nil, "src"), "omt://host:6400/src"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got, want := sourceURL("", 6400, []string{"::1"}, "src"), "omt://[::1]:6400/src"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAdvertiseHost(t *testing.T) {
	h := advertiseHost()
	if h == "" || len(h) > 63 || strings.ContainsAny(h, " .()") || !strings.HasSuffix(h, "-omt") {
		t.Fatalf("invalid advertise host %q", h)
	}
}

func TestDiscoveryServerRegistersNullTerminatedAddress(t *testing.T) {
	srv, err := ListenDiscoveryServer("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(rawMetadataFrame(Address{Name: "HOST (Null)", Port: 6500}.XML(), true)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range srv.Snapshot() {
			if a.Name == "HOST (Null)" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("null-terminated OMTAddress was not registered")
}

func TestNormalizeIPv4Mapped(t *testing.T) {
	if got := normalizeIP("::ffff:127.0.0.1"); got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeIP("::1"); got != "::1" {
		t.Fatalf("got %q", got)
	}
}
