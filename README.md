# omt-go

Pure Go implementation of [Open Media Transport](https://github.com/openmediatransport) 1.0 — video, audio and metadata over TCP with DNS-SD and an optional discovery server.

No cgo, no libomtnet linkage. The codec is [vmx-go](https://github.com/MikanseiLaboratory/vmx-go).

## Features

- OMT 1.0 frame headers and video/audio/metadata framing
- VMX1 video and FPA1 planar float audio
- Subscribe / Preview / Quality / Tally / OMTInfo metadata
- DNS-SD (`_omt._tcp`) advertise and browse
- OMT Discovery Server and client (`cmd/omt-discovery-server`)
- Gio example: monitor and animated test-pattern sender with 1 kHz audio

## Status

v0.1 is the minimum needed for the interoperability tests:

| Path | Status |
|---|---|
| Rust Test Patterns → Go Gio Monitor | works on macOS |
| Go Test Patterns → Rust Studio Monitor | works on macOS |
| Go sender/receiver loopback | tested |
| Discovery server relay | tested |
| Windows / Linux | CI build + user validation |

Not yet implemented: audio playback in the monitor, reconnect after a live
socket failure, alpha video, 1/8 preview receive, and TLS.

## Example

```sh
# Monitor: browse DNS-SD sources and display VMX1
GOEXPERIMENT=simd go run ./examples/gio-monitor -mode monitor \
  -url "omt://127.0.0.1:6400/Test Patterns"

# Test-pattern sender: animated colour bars + 1 kHz stereo
GOEXPERIMENT=simd go run ./examples/gio-monitor -mode send \
  -name "Go Test Patterns" -fps 30 -width 1920 -height 1080

# With a discovery server instead of DNS-SD
go run ./cmd/omt-discovery-server -port 6399
GOEXPERIMENT=simd go run ./examples/gio-monitor -mode monitor \
  -discovery-server omt://127.0.0.1:6399
```

The example requires Go 1.27 and, on macOS/Linux, the Gio platform libraries.
SIMD requires `GOEXPERIMENT=simd`; without it the scalar codec is used.

## Protocol notes

- Little-endian fields.
- 16-byte common header, 32-byte video header, 24-byte audio header.
- Video `DataLength` includes the 32-byte extended header, payload, and
  per-frame metadata.
- `MetadataLength` includes the trailing NUL for per-frame metadata.
- The four Tally strings are matched verbatim, including the libomtnet
  `Program==` quirk.
- Sender listens on 6400–6600 and advertises `_omt._tcp` as `HOST (Source)`.

## License

MIT.
