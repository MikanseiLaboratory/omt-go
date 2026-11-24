package omt

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Receiver connects to an OMT sender and delivers wire frames.
type Receiver struct {
	conn      net.Conn
	audioConn net.Conn
	videos    chan VideoFrame
	audios    chan AudioFrame
	info      atomic.Value // SenderInfo
	closed    chan struct{}
	once      sync.Once

	frames  atomic.Uint64
	dropped atomic.Uint64
	bytes   atomic.Uint64
	errors  atomic.Uint64
}

var _ io.Closer = (*Receiver)(nil)

func Dial(url string, opts ...Option) (*Receiver, error) {
	o := newOptions(opts...)
	host, port, _, err := ParseURL(url)
	if err != nil {
		return nil, err
	}
	if port == 0 {
		return nil, fmt.Errorf("omt: URL has no port")
	}
	d := net.Dialer{Timeout: time.Duration(o.connectTimeoutMS) * time.Millisecond}
	conn, err := d.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, err
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	r := &Receiver{
		conn: conn, videos: make(chan VideoFrame, 3), audios: make(chan AudioFrame, 8),
		closed: make(chan struct{}),
	}
	// Match libomtnet's handshake: metadata subscription, preview state, video
	// subscription, then the suggested quality.
	for _, xml := range []string{SubscribeMetaXML, previewXML(o.preview), SubscribeVideoXML, QualityXML(o.quality)} {
		if err := r.sendMeta(xml); err != nil {
			conn.Close()
			return nil, err
		}
	}

	if o.audio {
		ac, err := d.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("omt: audio connection: %w", err)
		}
		r.audioConn = ac
		if tcp, ok := ac.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true)
		}
		if err := writeFrame(ac, EncodeMetadataFrame(0, SubscribeAudioXML)); err != nil {
			conn.Close()
			ac.Close()
			return nil, fmt.Errorf("omt: subscribe audio: %w", err)
		}
		go r.readAudio()
	}
	go r.readVideo()
	return r, nil
}

func previewXML(on bool) string {
	if on {
		return PreviewOnXML
	}
	return PreviewOffXML
}

func (r *Receiver) sendMeta(xml string) error {
	return writeFrame(r.conn, EncodeMetadataFrame(0, xml))
}

func writeFrame(w net.Conn, b []byte) error {
	_, err := w.Write(b)
	return err
}

func (r *Receiver) Videos() <-chan VideoFrame { return r.videos }
func (r *Receiver) Audios() <-chan AudioFrame { return r.audios }
func (r *Receiver) Info() SenderInfo {
	if v := r.info.Load(); v != nil {
		return v.(SenderInfo)
	}
	return SenderInfo{}
}

func (r *Receiver) readVideo() {
	defer close(r.videos)
	for {
		f, err := ReadFrame(r.conn)
		if err != nil {
			return
		}
		r.bytes.Add(uint64(HeaderSize + len(f.Data) + len(f.Metadata)))
		if f.Header.Type == FrameMetadata {
			if info, ok := ParseSenderInfo(f.XML()); ok {
				r.info.Store(info)
			}
			continue
		}
		if f.Video == nil {
			continue
		}
		if f.Video.Codec != CodecVMX1 {
			r.errors.Add(1)
			continue
		}
		vf := VideoFrame{
			Width: int(f.Video.Width), Height: int(f.Video.Height),
			FrameRateN: int(f.Video.FrameRateN), FrameRateD: int(f.Video.FrameRateD),
			Aspect: f.Video.Aspect, Flags: f.Video.Flags, ColorSpace: f.Video.ColorSpace,
			Timestamp: f.Header.Timestamp, Data: append([]byte(nil), f.Data...),
			Metadata: f.MetadataString(),
		}
		select {
		case r.videos <- vf:
		default:
			select {
			case <-r.videos:
			default:
			}
			select {
			case r.videos <- vf:
			default:
			}
			r.dropped.Add(1)
		}
		r.frames.Add(1)
	}
}

func (r *Receiver) readAudio() {
	defer close(r.audios)
	for {
		f, err := ReadFrame(r.audioConn)
		if err != nil {
			return
		}
		if f.Audio == nil {
			continue
		}
		af := AudioFrame{
			SampleRate: int(f.Audio.SampleRate), SamplesPerChannel: int(f.Audio.SamplesPerChannel),
			Channels: int(f.Audio.Channels), ActiveChannels: f.Audio.ActiveChannels,
			Timestamp: f.Header.Timestamp, Data: append([]byte(nil), f.Data...),
			Metadata: f.MetadataString(),
		}
		select {
		case r.audios <- af:
		default:
			select {
			case <-r.audios:
			default:
			}
			select {
			case r.audios <- af:
			default:
			}
		}
	}
}

func (r *Receiver) Stats() Stats {
	return Stats{FramesReceived: r.frames.Load(), FramesDropped: r.dropped.Load(),
		BytesReceived: r.bytes.Load(), DecodeErrors: r.errors.Load()}
}

func (r *Receiver) Close() error {
	r.once.Do(func() {
		close(r.closed)
		if r.conn != nil {
			_ = r.conn.Close()
		}
		if r.audioConn != nil {
			_ = r.audioConn.Close()
		}
	})
	return nil
}
