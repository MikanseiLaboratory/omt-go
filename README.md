# omt-go

Pure Go implementation of [Open Media Transport (OMT) 1.0](https://github.com/openmediatransport).
No cgo. VMX1 video is provided by [vmx-go](https://github.com/MikanseiLaboratory/vmx-go).

## Features

- OMT 1.0 frame headers and video/audio/metadata framing
- VMX1 video and FPA1 planar float audio
- `image.RGBA` output for received video (`VideoFrame.RGBA`, `VideoDecoder`)
- Subscribe / Preview / Quality / Tally / OMTInfo metadata
- DNS-SD (`_omt._tcp`) advertise and browse
- OMT discovery server and client (`cmd/omt-discovery-server`)
- Gio example: monitor and test-pattern sender

## Quick start

```sh
# Monitor a source
GOEXPERIMENT=simd go run ./cmd/gio-monitor -mode monitor \
  -url "omt://127.0.0.1:6400/Test Patterns"

# Send color bars and a 1 kHz tone
GOEXPERIMENT=simd go run ./cmd/gio-monitor -mode send \
  -name "Go Test Patterns" -fps 30 -width 1920 -height 1080

# Use a discovery server instead of DNS-SD
go run ./cmd/omt-discovery-server -port 6399
GOEXPERIMENT=simd go run ./cmd/gio-monitor -mode monitor \
  -discovery-server omt://127.0.0.1:6399
```

## Decoding frames

`VideoFrame.RGBA` decodes a received VMX1 frame into a new `image.RGBA`:

```go
for f := range rx.Videos() {
    if img := f.RGBA(); img != nil { // nil when the frame cannot be decoded
        // upload img, e.g. paint.NewImageOp(img) with Gio
    }
}
```

For a stream of frames, reuse a `VideoDecoder` instead of allocating a codec
and buffer per frame:

```go
var dec omt.VideoDecoder
for f := range rx.Videos() {
    img, err := dec.Decode(f) // img is reused until the next Decode
    if err != nil {
        continue
    }
    _ = img
}
```

## Requirements

Go 1.27 or newer. SIMD is optional; without `GOEXPERIMENT=simd` the scalar
VMX1 path is used. The example needs the usual Gio platform libraries on
Linux/macOS.

## Status

Tested with Rust OMT tools and on Windows. See [docs/INTEROP.md](docs/INTEROP.md)
and [docs/WINDOWS_TEST.md](docs/WINDOWS_TEST.md).

Not implemented: audio playback in the monitor, automatic reconnect, alpha
video, 1/8 preview receive, TLS.

## Protocol notes

- Little-endian fields.
- 16-byte common header, 32-byte video header, 24-byte audio header.
- `DataLength` includes the extended header, payload, and per-frame metadata.
- `MetadataLength` includes the trailing NUL for per-frame metadata.
- Tally strings are matched verbatim, including the libomtnet `Program==`
  quirk.
- Sender listens on 6400–6600 and advertises `_omt._tcp` as `HOST (Source)`.

## License

MIT.
