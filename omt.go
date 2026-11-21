// Package omt implements the Open Media Transport 1.0 protocol.
package omt

import (
	"errors"
	"fmt"
)

const (
	DNSDServiceType      = "_omt._tcp"
	DNSDServiceTypeLocal = "_omt._tcp.local"
	NetworkPortStart     = 6400
	NetworkPortEnd       = 6600
	DiscoveryServerPort  = 6399
	ProtocolVersion      = 1
	HeaderSize           = 16
	VideoExtHeaderSize   = 32
	AudioExtHeaderSize   = 24
	MaxVideoSize         = 10 * 1024 * 1024
	MaxAudioSize         = 1024 * 1024
	MaxMetadataSize      = 65536
	MaxFrameSize         = 12 * 1024 * 1024
)

// FrameType is the OMT frame type bitmask.
type FrameType uint8

const (
	FrameMetadata FrameType = 1
	FrameVideo    FrameType = 2
	FrameAudio    FrameType = 4
)

func (f FrameType) Has(o FrameType) bool { return f&o != 0 }

// Codec is an OMT FourCC.
type Codec int32

const (
	CodecVMX1 Codec = 0x31584D56 // "VMX1"
	CodecFPA1 Codec = 0x31415046 // "FPA1"
	CodecUYVY Codec = 0x59565955 // "UYVY"
	CodecYUY2 Codec = 0x32595559 // "YUY2"
	CodecBGRA Codec = 0x41524742 // "BGRA"
	CodecNV12 Codec = 0x3231564E // "NV12"
	CodecYV12 Codec = 0x32315659 // "YV12"
)

func FourCC(s string) Codec {
	var v uint32
	for i := 0; i < 4 && i < len(s); i++ {
		v |= uint32(s[i]) << (8 * i)
	}
	return Codec(v)
}

func (c Codec) String() string {
	b := []byte{byte(c), byte(c >> 8), byte(c >> 16), byte(c >> 24)}
	return string(b)
}

// ColorSpace follows the OMT flag values.
type ColorSpace int32

const (
	ColorUndefined ColorSpace = 0
	ColorBT601     ColorSpace = 601
	ColorBT709     ColorSpace = 709
)

// VideoFlags are the OMT video flags.
type VideoFlags int32

const (
	VideoInterlaced    VideoFlags = 1
	VideoAlpha         VideoFlags = 2
	VideoPremultiplied VideoFlags = 4
	VideoPreview       VideoFlags = 8
	VideoHighBitDepth  VideoFlags = 16
)

func (v VideoFlags) Has(o VideoFlags) bool { return v&o != 0 }

// Quality is the suggested encode quality.
type Quality int32

const (
	QualityDefault Quality = 0
	QualityLow     Quality = 1
	QualityMedium  Quality = 50
	QualityHigh    Quality = 100
)

// SenderInfo is the OMTInfo metadata.
type SenderInfo struct {
	ProductName  string
	Manufacturer string
	Version      string
}

// Tally is receiver tally state.
type Tally struct {
	Preview int32
	Program int32
}

// FrameHeader is the common 16-byte header.
type FrameHeader struct {
	Version        uint8
	Type           FrameType
	Timestamp      int64
	MetadataLength uint16
	DataLength     int32
}

// VideoHeader is the 32-byte video extended header.
type VideoHeader struct {
	Codec      Codec
	Width      int32
	Height     int32
	FrameRateN int32
	FrameRateD int32
	Aspect     float32
	Flags      VideoFlags
	ColorSpace ColorSpace
}

// AudioHeader is the 24-byte audio extended header.
type AudioHeader struct {
	Codec             Codec
	SampleRate        int32
	SamplesPerChannel int32
	Channels          int32
	ActiveChannels    uint32
	Reserved          int32
}

// VideoFrame is an encoded VMX1 frame on the wire.
type VideoFrame struct {
	Width      int
	Height     int
	FrameRateN int
	FrameRateD int
	Aspect     float32
	Flags      VideoFlags
	ColorSpace ColorSpace
	Timestamp  int64
	Data       []byte
	Metadata   string
}

// AudioFrame is a planar float32 FPA1 frame.
type AudioFrame struct {
	SampleRate        int
	SamplesPerChannel int
	Channels          int
	ActiveChannels    uint32
	Timestamp         int64
	Data              []byte
	Metadata          string
}

// Source is a discovered OMT source.
type Source struct {
	Name      string
	Host      string
	Port      int
	Addresses []string
	URL       string
}

// ErrProtocol indicates malformed OMT data.
var ErrProtocol = errors.New("omt: protocol error")

func protocolErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrProtocol, fmt.Sprintf(format, args...))
}
