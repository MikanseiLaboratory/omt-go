package omt

import "time"

// ServerBrowser lists sources relayed by an OMT discovery server.
type ServerBrowser struct {
	client *DiscoveryClient
}

func NewServerBrowser(url string) (*ServerBrowser, error) {
	c, err := NewDiscoveryClient(url)
	if err != nil {
		return nil, err
	}
	return &ServerBrowser{client: c}, nil
}

func (b *ServerBrowser) Refresh(wait time.Duration) []Source {
	if wait > 0 {
		time.Sleep(wait)
	}
	addrs := b.client.Sources()
	out := make([]Source, 0, len(addrs))
	for _, a := range addrs {
		host, source := SplitInstance(a.Name)
		ip := host
		if len(a.Addresses) > 0 {
			ip = a.Addresses[0]
		}
		url := "omt://" + ip + ":" + itoa(a.Port) + "/" + source
		out = append(out, Source{Name: a.Name, Host: host, Port: a.Port, Addresses: a.Addresses, URL: url})
	}
	return out
}

func (b *ServerBrowser) Sources() []Source { return b.Refresh(0) }
func (b *ServerBrowser) Close() {
	if b != nil && b.client != nil {
		_ = b.client.Close()
	}
}
