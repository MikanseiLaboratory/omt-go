package omt

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Sender listens for OMT receivers and sends video/audio/metadata.
type Sender struct {
	instance string
	info     SenderInfo
	bind     string
	port     int

	listener   net.Listener
	advertiser *Advertiser
	disc       *DiscoveryClient
	discURL    string

	mu     sync.Mutex
	peers  map[*peer]struct{}
	closed bool

	framesSent    atomic.Uint64
	framesDropped atomic.Uint64
	bytesSent     atomic.Uint64
	closing       chan struct{}
}

type peer struct {
	conn net.Conn
	send chan []byte
	done chan struct{}
	once sync.Once

	mu    sync.Mutex
	video bool
	audio bool
}

func NewSender(name string, opts ...Option) (*Sender, error) {
	o := newOptions(opts...)
	if name == "" {
		name = "Go Test Patterns"
	}
	s := &Sender{
		instance: LocalInstanceName(name), bind: o.bind, port: o.port,
		peers: map[*peer]struct{}{}, closing: make(chan struct{}),
		discURL: o.discoveryServer,
	}
	s.info = o.info
	if s.info.ProductName == "" {
		s.info = SenderInfo{ProductName: "omt-go " + name, Manufacturer: "MikanseiLaboratory", Version: "0.1.0"}
	}
	ln, port, err := listenRange(o.bind, o.port)
	if err != nil {
		return nil, err
	}
	s.listener = ln
	s.port = port
	if o.advertise {
		if o.discoveryServer != "" {
			dc, err := NewDiscoveryClient(o.discoveryServer)
			if err != nil {
				s.listener.Close()
				return nil, fmt.Errorf("omt: discovery server: %w", err)
			}
			s.disc = dc
			if err := dc.Register(Address{Name: s.instance, Port: s.port}); err != nil {
				dc.Close()
				s.listener.Close()
				return nil, fmt.Errorf("omt: discovery register: %w", err)
			}
		} else {
			adv, err := NewAdvertiser(s.instance, s.port)
			if err != nil {
				s.listener.Close()
				return nil, err
			}
			s.advertiser = adv
		}
	}
	go s.acceptLoop()
	return s, nil
}

func listenRange(bind string, port int) (net.Listener, int, error) {
	try := func(p int) (net.Listener, error) {
		return net.Listen("tcp", net.JoinHostPort(bind, fmt.Sprint(p)))
	}
	if port > 0 {
		ln, err := try(port)
		return ln, port, err
	}
	var last error
	for p := NetworkPortStart; p <= NetworkPortEnd; p++ {
		ln, err := try(p)
		if err == nil {
			return ln, p, nil
		}
		last = err
	}
	return nil, 0, last
}

func (s *Sender) Port() int        { return s.port }
func (s *Sender) Instance() string { return s.instance }
func (s *Sender) Info() SenderInfo { return s.info }

func (s *Sender) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true)
		}
		p := &peer{conn: conn, send: make(chan []byte, 4), done: make(chan struct{})}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			conn.Close()
			return
		}
		s.peers[p] = struct{}{}
		s.mu.Unlock()
		// libomtnet sends OMTInfo immediately after accept.
		s.enqueue(p, EncodeMetadataFrame(0, s.info.XML()))
		go p.writeLoop()
		go func() {
			s.readLoop(p)
			s.removePeer(p)
		}()
	}
}

func (p *peer) writeLoop() {
	for {
		select {
		case <-p.done:
			return
		case b := <-p.send:
			if _, err := p.conn.Write(b); err != nil {
				p.close()
				return
			}
		}
	}
}

func (p *peer) close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.conn.Close()
	})
}

func (s *Sender) readLoop(p *peer) {
	for {
		f, err := ReadFrame(p.conn)
		if err != nil {
			return
		}
		xml := f.XML()
		if sub, ok := ParseSubscribe(xml); ok {
			p.mu.Lock()
			if sub.Video {
				p.video = true
			}
			if sub.Audio {
				p.audio = true
			}
			p.mu.Unlock()
			continue
		}
		if _, ok := ParseSettings(xml); ok {
			continue
		}
		if _, ok := ParseTally(xml); ok {
			continue
		}
	}
}

func (s *Sender) removePeer(p *peer) {
	p.close()
	s.mu.Lock()
	delete(s.peers, p)
	s.mu.Unlock()
}

func (p *peer) subscribed(video bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if video {
		return p.video
	}
	return p.audio
}

func (s *Sender) enqueue(p *peer, b []byte) {
	select {
	case p.send <- b:
	default:
		s.framesDropped.Add(1)
	}
}

func (s *Sender) broadcast(b []byte, video bool) {
	s.mu.Lock()
	peers := make([]*peer, 0, len(s.peers))
	for p := range s.peers {
		if p.subscribed(video) {
			peers = append(peers, p)
		}
	}
	s.mu.Unlock()
	if len(peers) == 0 {
		return
	}
	for _, p := range peers {
		s.enqueue(p, b)
	}
	s.framesSent.Add(1)
	s.bytesSent.Add(uint64(len(b)))
}

// SendVideo sends one encoded VMX1 frame to subscribed receivers.
func (s *Sender) SendVideo(v VideoFrame) error {
	if v.Width <= 0 || v.Height <= 0 {
		return fmt.Errorf("omt: invalid video dimensions %dx%d", v.Width, v.Height)
	}
	v.Metadata = trimMetadataNUL(v.Metadata)
	if err := validateFrameSize(VideoExtHeaderSize, len(v.Data), v.Metadata, MaxVideoSize); err != nil {
		return err
	}
	if v.Aspect == 0 {
		v.Aspect = float32(v.Width) / float32(v.Height)
	}
	s.broadcast(EncodeVideoFrame(v), true)
	return nil
}

// SendAudio sends one FPA1 frame.
func (s *Sender) SendAudio(a AudioFrame) error {
	a.Metadata = trimMetadataNUL(a.Metadata)
	if err := validateFrameSize(AudioExtHeaderSize, len(a.Data), a.Metadata, MaxAudioSize); err != nil {
		return err
	}
	s.broadcast(EncodeAudioFrame(a), false)
	return nil
}

// SendMetadata sends a metadata XML frame to all peers.
func (s *Sender) SendMetadata(xml string) error {
	xml = trimMetadataNUL(xml)
	if len(xml) > MaxMetadataSize {
		return fmt.Errorf("omt: metadata too large (%d > %d)", len(xml), MaxMetadataSize)
	}
	s.broadcastAll(EncodeMetadataFrame(0, xml))
	return nil
}

func trimMetadataNUL(s string) string {
	for len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return s
}

func validateFrameSize(extHeader, dataLen int, metadata string, maxData int) error {
	if dataLen > maxData {
		return fmt.Errorf("omt: frame data too large (%d > %d)", dataLen, maxData)
	}
	metaLen := 0
	if metadata != "" {
		metaLen = len(metadata) + 1 // includes the NUL terminator
	}
	if metaLen > MaxMetadataSize {
		return fmt.Errorf("omt: metadata too large (%d > %d)", metaLen, MaxMetadataSize)
	}
	if total := extHeader + dataLen + metaLen; total > MaxFrameSize {
		return fmt.Errorf("omt: frame too large (%d > %d)", total, MaxFrameSize)
	}
	return nil
}

func (s *Sender) broadcastAll(b []byte) {
	s.mu.Lock()
	peers := make([]*peer, 0, len(s.peers))
	for p := range s.peers {
		peers = append(peers, p)
	}
	s.mu.Unlock()
	for _, p := range peers {
		s.enqueue(p, b)
	}
	s.framesSent.Add(1)
	s.bytesSent.Add(uint64(len(b)))
}

func (s *Sender) Stats() Stats {
	s.mu.Lock()
	peers := len(s.peers)
	var v, a int
	for p := range s.peers {
		if p.subscribed(true) {
			v++
		}
		if p.subscribed(false) {
			a++
		}
	}
	s.mu.Unlock()
	return Stats{
		FramesSent: s.framesSent.Load(), FramesDropped: s.framesDropped.Load(),
		BytesSent: s.bytesSent.Load(), Peers: peers, VideoSubscribers: v, AudioSubscribers: a,
	}
}

func (s *Sender) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.closing)
	peers := make([]*peer, 0, len(s.peers))
	for p := range s.peers {
		peers = append(peers, p)
	}
	s.mu.Unlock()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if s.advertiser != nil {
		s.advertiser.Close()
	}
	if s.disc != nil {
		s.disc.Close()
	}
	for _, p := range peers {
		p.close()
	}
	s.mu.Lock()
	s.peers = map[*peer]struct{}{}
	s.mu.Unlock()
	return nil
}

// ParseURL parses omt://host:port/name. A bare host[:port][/name] is also
// accepted for convenience.
func ParseURL(url string) (host string, port int, name string, err error) {
	rest := url
	if strings.HasPrefix(url, "omt://") {
		rest = strings.TrimPrefix(url, "omt://")
	} else if strings.Contains(url, "://") {
		return "", 0, "", fmt.Errorf("omt: invalid URL %q", url)
	}
	hostPort := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		hostPort = rest[:i]
		name = rest[i+1:]
	}
	if hostPort == "" {
		return "", 0, "", fmt.Errorf("omt: invalid URL %q", url)
	}
	if h, p, e := net.SplitHostPort(hostPort); e == nil {
		host = h
		port, err = strconv.Atoi(p)
		if err != nil || port < 0 || port > 65535 {
			return "", 0, "", fmt.Errorf("omt: invalid port in URL %q", url)
		}
	} else if strings.HasPrefix(hostPort, "[") {
		return "", 0, "", fmt.Errorf("omt: invalid URL %q", url)
	} else {
		if strings.Contains(hostPort, ":") {
			return "", 0, "", fmt.Errorf("omt: invalid URL %q", url)
		}
		host = hostPort
	}
	if host == "" {
		return "", 0, "", fmt.Errorf("omt: invalid URL %q", url)
	}
	return host, port, name, nil
}
