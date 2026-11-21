package omt

// Stats is a point-in-time sender/receiver snapshot.
type Stats struct {
	FramesSent       uint64
	FramesReceived   uint64
	FramesDropped    uint64
	BytesSent        uint64
	BytesReceived    uint64
	Peers            int
	VideoSubscribers int
	AudioSubscribers int
	DecodeErrors     uint64
}
