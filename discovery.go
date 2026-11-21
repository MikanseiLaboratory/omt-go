package omt

import (
	"context"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brutella/dnssd"
)

// LocalInstanceName returns "HOST (Source)" capped at 63 bytes.
func LocalInstanceName(source string) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "host"
	}
	if i := strings.IndexByte(host, '.'); i >= 0 {
		host = host[:i]
	}
	host = sanitizeHost(host)
	full := host + " (" + source + ")"
	if len(full) > 63 {
		keep := 63 - len(host) - 3
		if keep < 1 {
			keep = 1
		}
		source = source[:min(keep, len(source))]
		full = host + " (" + source + ")"
	}
	return full
}

func sanitizeHost(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "host"
	}
	return out
}

// SplitInstance splits "HOST (Source)".
func SplitInstance(name string) (host, source string) {
	name = strings.TrimSpace(name)
	open := strings.IndexByte(name, '(')
	close := strings.LastIndexByte(name, ')')
	if open >= 0 && close > open {
		return strings.TrimSpace(name[:open]), strings.TrimSpace(name[open+1 : close])
	}
	return "", name
}

// Advertiser publishes an _omt._tcp service via DNS-SD.
type Advertiser struct {
	responder dnssd.Responder
	handle    dnssd.ServiceHandle
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewAdvertiser(instance string, port int) (*Advertiser, error) {
	cfg := dnssd.Config{
		Name: instance, Type: DNSDServiceType, Domain: "local", Port: port,
	}
	srv, err := dnssd.NewService(cfg)
	if err != nil {
		return nil, err
	}
	rp, err := dnssd.NewResponder()
	if err != nil {
		return nil, err
	}
	h, err := rp.Add(srv)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &Advertiser{responder: rp, handle: h, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(a.done)
		_ = rp.Respond(ctx)
	}()
	return a, nil
}

func (a *Advertiser) Close() {
	if a == nil {
		return
	}
	if a.responder != nil && a.handle != nil {
		a.responder.Remove(a.handle)
	}
	if a.cancel != nil {
		a.cancel()
	}
	if a.done != nil {
		select {
		case <-a.done:
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Browser discovers _omt._tcp services.
type Browser struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	sources map[string]Source
	started bool
}

func NewBrowser() *Browser {
	ctx, cancel := context.WithCancel(context.Background())
	return &Browser{ctx: ctx, cancel: cancel, sources: map[string]Source{}}
}

func (b *Browser) ensureStarted() {
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return
	}
	b.started = true
	b.mu.Unlock()
	go func() {
		_ = dnssd.LookupType(b.ctx, DNSDServiceTypeLocal+".", func(e dnssd.BrowseEntry) {
			host, source := SplitInstance(e.Name)
			addrs := make([]string, 0, len(e.IPs))
			for _, ip := range e.IPs {
				addrs = append(addrs, ip.String())
			}
			sort.Strings(addrs)
			urlHost := host
			if urlHost == "" && len(addrs) > 0 {
				urlHost = addrs[0]
			}
			url := "omt://" + urlHost
			if e.Port > 0 {
				url += ":" + itoa(e.Port)
			}
			url += "/" + source
			b.mu.Lock()
			b.sources[e.Name] = Source{Name: e.Name, Host: host, Port: e.Port, Addresses: addrs, URL: url}
			b.mu.Unlock()
		}, func(e dnssd.BrowseEntry) {
			b.mu.Lock()
			delete(b.sources, e.Name)
			b.mu.Unlock()
		})
	}()
}

// Refresh waits up to wait and returns the current sources.
func (b *Browser) Refresh(wait time.Duration) []Source {
	b.ensureStarted()
	if wait > 0 {
		time.Sleep(wait)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Source, 0, len(b.sources))
	for _, s := range b.sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (b *Browser) Sources() []Source { return b.Refresh(0) }

func (b *Browser) Close() {
	if b == nil {
		return
	}
	if b.cancel != nil {
		b.cancel()
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// InterfaceAddresses returns non-loopback IPv4 addresses for diagnostics.
func InterfaceAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}
