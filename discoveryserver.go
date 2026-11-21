package omt

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// DiscoveryServer relays OMTAddress registrations between clients.
type DiscoveryServer struct {
	ln      net.Listener
	mu      sync.Mutex
	peers   map[*serverPeer]struct{}
	entries map[string]serverEntry
	closed  bool
	done    chan struct{}
}

type serverEntry struct {
	addr  Address
	owner *serverPeer
}

type serverPeer struct {
	conn net.Conn
	mu   sync.Mutex
}

// ListenDiscoveryServer starts a discovery server. An empty bind uses all interfaces.
func ListenDiscoveryServer(bind string, port int) (*DiscoveryServer, error) {
	if port == 0 {
		port = DiscoveryServerPort
	}
	addr := net.JoinHostPort(bind, fmt.Sprint(port))
	if bind == "" {
		addr = fmt.Sprintf(":%d", port)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Fall back to IPv4 if IPv6 dual-stack is unavailable.
		ln, err = net.Listen("tcp4", addr)
		if err != nil {
			return nil, err
		}
	}
	s := &DiscoveryServer{
		ln: ln, peers: map[*serverPeer]struct{}{}, entries: map[string]serverEntry{}, done: make(chan struct{}),
	}
	go s.acceptLoop()
	return s, nil
}

func (s *DiscoveryServer) Port() int {
	if s == nil || s.ln == nil {
		return 0
	}
	return s.ln.Addr().(*net.TCPAddr).Port
}

func (s *DiscoveryServer) Addr() string {
	if s == nil || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *DiscoveryServer) acceptLoop() {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		p := &serverPeer{conn: conn}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			conn.Close()
			return
		}
		s.peers[p] = struct{}{}
		initial := make([]Address, 0, len(s.entries))
		for _, e := range s.entries {
			initial = append(initial, e.addr)
		}
		s.mu.Unlock()
		go s.peerLoop(p)
		// Do not block the accept loop on a slow client's initial snapshot.
		go func() {
			for _, a := range initial {
				if err := p.write(EncodeMetadataFrame(0, a.XML())); err != nil {
					return
				}
			}
		}()
	}
}

func (p *serverPeer) write(b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// A stalled discovery client must not wedge the shared read loop that
	// relays registrations to every other client.
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := p.conn.Write(b)
	_ = p.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		_ = p.conn.Close()
	}
	return err
}

func (s *DiscoveryServer) peerLoop(p *serverPeer) {
	defer s.dropPeer(p)
	for {
		f, err := ReadFrame(p.conn)
		if err != nil {
			return
		}
		a, err := ParseAddress(f.XML())
		if err != nil {
			continue
		}
		s.handleAddress(p, a)
	}
}

func (s *DiscoveryServer) handleAddress(p *serverPeer, a Address) {
	key := a.Name + "\x00" + fmt.Sprint(a.Port)
	peerIP := ""
	if host, _, err := net.SplitHostPort(p.conn.RemoteAddr().String()); err == nil {
		peerIP = normalizeIP(host)
	}

	s.mu.Lock()
	existing, ok := s.entries[key]
	if a.Removed {
		if ok {
			delete(s.entries, key)
			removed := existing.addr
			removed.Removed = true
			peers := s.peerListLocked()
			s.mu.Unlock()
			b := EncodeMetadataFrame(0, removed.XML())
			go func() {
				for _, q := range peers {
					_ = q.write(b)
				}
			}()
			return
		}
		s.mu.Unlock()
		return
	}
	if ok {
		s.mu.Unlock()
		return
	}
	a.Addresses = []string{peerIP}
	s.entries[key] = serverEntry{addr: a, owner: p}
	peers := s.peerListLocked()
	s.mu.Unlock()
	b := EncodeMetadataFrame(0, a.XML())
	go func() {
		for _, q := range peers {
			_ = q.write(b)
		}
	}()
}

func (s *DiscoveryServer) peerListLocked() []*serverPeer {
	out := make([]*serverPeer, 0, len(s.peers))
	for q := range s.peers {
		out = append(out, q)
	}
	return out
}

func (s *DiscoveryServer) dropPeer(p *serverPeer) {
	_ = p.conn.Close()
	s.mu.Lock()
	delete(s.peers, p)
	var removed []Address
	for k, e := range s.entries {
		if e.owner == p {
			delete(s.entries, k)
			a := e.addr
			a.Removed = true
			removed = append(removed, a)
		}
	}
	peers := s.peerListLocked()
	s.mu.Unlock()
	for _, a := range removed {
		b := EncodeMetadataFrame(0, a.XML())
		go func() {
			for _, q := range peers {
				_ = q.write(b)
			}
		}()
	}
}

func (s *DiscoveryServer) Snapshot() []Address {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Address, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e.addr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Port < out[j].Port
	})
	return out
}

func (s *DiscoveryServer) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	peers := s.peerListLocked()
	s.mu.Unlock()
	_ = s.ln.Close()
	for _, p := range peers {
		_ = p.conn.Close()
	}
	<-s.done
	return nil
}

// DiscoveryClient registers sources with a discovery server and learns others.
type DiscoveryClient struct {
	conn    net.Conn
	mu      sync.Mutex
	writeMu sync.Mutex
	src     map[string]Address
	done    chan struct{}
	once    sync.Once
}

func NewDiscoveryClient(url string) (*DiscoveryClient, error) {
	host, port, _, err := ParseURL(url)
	if err != nil {
		return nil, err
	}
	if port == 0 {
		port = DiscoveryServerPort
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), 3e9)
	if err != nil {
		return nil, err
	}
	c := &DiscoveryClient{conn: conn, src: map[string]Address{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *DiscoveryClient) readLoop() {
	defer close(c.done)
	for {
		f, err := ReadFrame(c.conn)
		if err != nil {
			return
		}
		a, err := ParseAddress(f.XML())
		if err != nil {
			continue
		}
		key := a.Name + "\x00" + fmt.Sprint(a.Port)
		c.mu.Lock()
		if a.Removed {
			delete(c.src, key)
		} else {
			c.src[key] = a
		}
		c.mu.Unlock()
	}
}

func (c *DiscoveryClient) Register(a Address) error {
	a.Removed = false
	return c.writeFrame(EncodeMetadataFrame(0, a.XML()))
}

func (c *DiscoveryClient) Deregister(a Address) error {
	a.Removed = true
	return c.writeFrame(EncodeMetadataFrame(0, a.XML()))
}

func (c *DiscoveryClient) writeFrame(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.conn.Write(b)
	_ = c.conn.SetWriteDeadline(time.Time{})
	return err
}

func (c *DiscoveryClient) Sources() []Address {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Address, 0, len(c.src))
	for _, a := range c.src {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Port < out[j].Port
	})
	return out
}

func (c *DiscoveryClient) Close() error {
	if c == nil {
		return nil
	}
	c.once.Do(func() {
		_ = c.conn.Close()
		select {
		case <-c.done:
		case <-timeAfter(500):
		}
	})
	return nil
}

func timeAfter(ms int) <-chan struct{} {
	ch := make(chan struct{})
	go func() { time.Sleep(time.Duration(ms) * time.Millisecond); close(ch) }()
	return ch
}
