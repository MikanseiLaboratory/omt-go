package omt

import (
	"encoding/binary"
	"io"
	"math"
)

// Frame is a decoded OMT frame.
type Frame struct {
	Header   FrameHeader
	Video    *VideoHeader
	Audio    *AudioHeader
	Data     []byte
	Metadata []byte
}

func (f Frame) MetadataString() string {
	end := len(f.Metadata)
	for i, b := range f.Metadata {
		if b == 0 {
			end = i
			break
		}
	}
	return string(f.Metadata[:end])
}

// XML returns the control XML carried by the frame. Metadata frames put the
// document in Data (some implementations NUL-terminate it), while media frames
// carry it in the per-frame metadata trailer.
func (f Frame) XML() string {
	if s := f.MetadataString(); s != "" {
		return s
	}
	if f.Header.Type == FrameMetadata {
		return metadataXML(f.Data)
	}
	return ""
}

func appendHeader(dst []byte, h FrameHeader) []byte {
	var b [HeaderSize]byte
	b[0] = h.Version
	b[1] = byte(h.Type)
	binary.LittleEndian.PutUint64(b[2:10], uint64(h.Timestamp))
	binary.LittleEndian.PutUint16(b[10:12], h.MetadataLength)
	binary.LittleEndian.PutUint32(b[12:16], uint32(h.DataLength))
	return append(dst, b[:]...)
}

func appendVideoHeader(dst []byte, v VideoHeader) []byte {
	var b [VideoExtHeaderSize]byte
	binary.LittleEndian.PutUint32(b[0:4], uint32(v.Codec))
	binary.LittleEndian.PutUint32(b[4:8], uint32(v.Width))
	binary.LittleEndian.PutUint32(b[8:12], uint32(v.Height))
	binary.LittleEndian.PutUint32(b[12:16], uint32(v.FrameRateN))
	binary.LittleEndian.PutUint32(b[16:20], uint32(v.FrameRateD))
	binary.LittleEndian.PutUint32(b[20:24], math.Float32bits(v.Aspect))
	binary.LittleEndian.PutUint32(b[24:28], uint32(v.Flags))
	binary.LittleEndian.PutUint32(b[28:32], uint32(v.ColorSpace))
	return append(dst, b[:]...)
}

func appendAudioHeader(dst []byte, a AudioHeader) []byte {
	var b [AudioExtHeaderSize]byte
	binary.LittleEndian.PutUint32(b[0:4], uint32(a.Codec))
	binary.LittleEndian.PutUint32(b[4:8], uint32(a.SampleRate))
	binary.LittleEndian.PutUint32(b[8:12], uint32(a.SamplesPerChannel))
	binary.LittleEndian.PutUint32(b[12:16], uint32(a.Channels))
	binary.LittleEndian.PutUint32(b[16:20], a.ActiveChannels)
	binary.LittleEndian.PutUint32(b[20:24], uint32(a.Reserved))
	return append(dst, b[:]...)
}

// EncodeVideoFrame serializes one video frame.
func EncodeVideoFrame(v VideoFrame) []byte {
	metaLen := 0
	if v.Metadata != "" {
		metaLen = len(v.Metadata) + 1
	}
	dataLen := VideoExtHeaderSize + len(v.Data) + metaLen
	h := FrameHeader{
		Version: ProtocolVersion, Type: FrameVideo, Timestamp: v.Timestamp,
		MetadataLength: uint16(metaLen), DataLength: int32(dataLen),
	}
	dst := make([]byte, 0, HeaderSize+dataLen)
	dst = appendHeader(dst, h)
	dst = appendVideoHeader(dst, VideoHeader{
		Codec: CodecVMX1, Width: int32(v.Width), Height: int32(v.Height),
		FrameRateN: int32(v.FrameRateN), FrameRateD: int32(v.FrameRateD),
		Aspect: v.Aspect, Flags: v.Flags, ColorSpace: v.ColorSpace,
	})
	dst = append(dst, v.Data...)
	if v.Metadata != "" {
		dst = append(dst, v.Metadata...)
		dst = append(dst, 0)
	}
	return dst
}

// EncodeAudioFrame serializes one audio frame.
func EncodeAudioFrame(a AudioFrame) []byte {
	metaLen := 0
	if a.Metadata != "" {
		metaLen = len(a.Metadata) + 1
	}
	dataLen := AudioExtHeaderSize + len(a.Data) + metaLen
	h := FrameHeader{
		Version: ProtocolVersion, Type: FrameAudio, Timestamp: a.Timestamp,
		MetadataLength: uint16(metaLen), DataLength: int32(dataLen),
	}
	dst := make([]byte, 0, HeaderSize+dataLen)
	dst = appendHeader(dst, h)
	dst = appendAudioHeader(dst, AudioHeader{
		Codec: CodecFPA1, SampleRate: int32(a.SampleRate),
		SamplesPerChannel: int32(a.SamplesPerChannel), Channels: int32(a.Channels),
		ActiveChannels: a.ActiveChannels,
	})
	dst = append(dst, a.Data...)
	if a.Metadata != "" {
		dst = append(dst, a.Metadata...)
		dst = append(dst, 0)
	}
	return dst
}

// EncodeMetadataFrame serializes a metadata XML frame.
func EncodeMetadataFrame(timestamp int64, xml string) []byte {
	data := []byte(xml)
	h := FrameHeader{
		Version: ProtocolVersion, Type: FrameMetadata, Timestamp: timestamp,
		DataLength: int32(len(data)),
	}
	dst := make([]byte, 0, HeaderSize+len(data))
	dst = appendHeader(dst, h)
	return append(dst, data...)
}

// ReadFrame reads one complete OMT frame.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hb [HeaderSize]byte
	if _, err := io.ReadFull(r, hb[:]); err != nil {
		return nil, err
	}
	h := FrameHeader{
		Version: hb[0], Type: FrameType(hb[1]),
		Timestamp:      int64(binary.LittleEndian.Uint64(hb[2:10])),
		MetadataLength: binary.LittleEndian.Uint16(hb[10:12]),
		DataLength:     int32(binary.LittleEndian.Uint32(hb[12:16])),
	}
	if h.Version != ProtocolVersion {
		return nil, protocolErr("unsupported version %d", h.Version)
	}
	if h.DataLength < 0 || h.DataLength > MaxFrameSize {
		return nil, protocolErr("invalid data length %d", h.DataLength)
	}
	body := make([]byte, h.DataLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	if int(h.MetadataLength) > len(body) {
		return nil, protocolErr("metadata length exceeds body")
	}
	metaStart := len(body) - int(h.MetadataLength)
	f := &Frame{Header: h, Metadata: body[metaStart:]}
	payload := body[:metaStart]
	switch h.Type {
	case FrameVideo:
		if len(payload) < VideoExtHeaderSize {
			return nil, protocolErr("video header too short")
		}
		f.Video = &VideoHeader{
			Codec:      Codec(int32(binary.LittleEndian.Uint32(payload[0:4]))),
			Width:      int32(binary.LittleEndian.Uint32(payload[4:8])),
			Height:     int32(binary.LittleEndian.Uint32(payload[8:12])),
			FrameRateN: int32(binary.LittleEndian.Uint32(payload[12:16])),
			FrameRateD: int32(binary.LittleEndian.Uint32(payload[16:20])),
			Aspect:     math.Float32frombits(binary.LittleEndian.Uint32(payload[20:24])),
			Flags:      VideoFlags(int32(binary.LittleEndian.Uint32(payload[24:28]))),
			ColorSpace: ColorSpace(int32(binary.LittleEndian.Uint32(payload[28:32]))),
		}
		f.Data = payload[VideoExtHeaderSize:]
	case FrameAudio:
		if len(payload) < AudioExtHeaderSize {
			return nil, protocolErr("audio header too short")
		}
		f.Audio = &AudioHeader{
			Codec:             Codec(int32(binary.LittleEndian.Uint32(payload[0:4]))),
			SampleRate:        int32(binary.LittleEndian.Uint32(payload[4:8])),
			SamplesPerChannel: int32(binary.LittleEndian.Uint32(payload[8:12])),
			Channels:          int32(binary.LittleEndian.Uint32(payload[12:16])),
			ActiveChannels:    binary.LittleEndian.Uint32(payload[16:20]),
			Reserved:          int32(binary.LittleEndian.Uint32(payload[20:24])),
		}
		f.Data = payload[AudioExtHeaderSize:]
	case FrameMetadata:
		f.Data = payload
	default:
		return nil, protocolErr("unknown frame type %d", h.Type)
	}
	return f, nil
}
