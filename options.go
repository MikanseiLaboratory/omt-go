package omt

// Option configures Sender/Receiver.
type Option func(*options)

type options struct {
	bind             string
	port             int
	advertise        bool
	info             SenderInfo
	discoveryServer  string
	preview          bool
	quality          Quality
	audio            bool
	connectTimeoutMS int
}

func defaultOptions() options {
	return options{bind: "0.0.0.0", advertise: true, quality: QualityDefault, audio: false, connectTimeoutMS: 5000}
}

func WithBind(host string) Option           { return func(o *options) { o.bind = host } }
func WithPort(port int) Option              { return func(o *options) { o.port = port } }
func WithAdvertise(v bool) Option           { return func(o *options) { o.advertise = v } }
func WithInfo(i SenderInfo) Option          { return func(o *options) { o.info = i } }
func WithDiscoveryServer(url string) Option { return func(o *options) { o.discoveryServer = url } }
func WithPreview(v bool) Option             { return func(o *options) { o.preview = v } }
func WithQuality(q Quality) Option          { return func(o *options) { o.quality = q } }
func WithAudio(v bool) Option               { return func(o *options) { o.audio = v } }

func newOptions(opts ...Option) options {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return o
}
