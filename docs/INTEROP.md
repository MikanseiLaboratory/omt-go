# Interoperability

Tested on macOS with the Rust OMT Community Tools (release builds).

## Rust Test Patterns → Go Gio Monitor

```sh
GOEXPERIMENT=simd go run ./examples/gio-monitor -mode monitor \
  -url "omt://127.0.0.1:6400/Test Pattern 2!"
```

Observed: 1280×720, ~67 fps receive, ~7.5 ms decode, 0 drops.

## Go Test Patterns → Rust Studio Monitor

```sh
GOEXPERIMENT=simd go run ./examples/gio-monitor -mode send \
  -name "Go Test Patterns" -fps 30
./omt-studio-monitor --url "omt://127.0.0.1:6400/Go Test Patterns"
```

Observed in Studio Monitor: 1920×1080 source, decoded frames, CPU decode
~1.9 ms, 48 kHz stereo audio packets.

## Go ↔ Go through the discovery server

```sh
go run ./cmd/omt-discovery-server -port 6399
GOEXPERIMENT=simd go run ./examples/gio-monitor -mode send -port 6500 \
  -discovery-server omt://127.0.0.1:6399
GOEXPERIMENT=simd go run ./examples/gio-monitor -mode monitor \
  -discovery-server omt://127.0.0.1:6399 \
  -url "omt://127.0.0.1:6500/Go Test Patterns"
```

Observed: source list contains `HOST (Go Test Patterns)`, 1920×1080 at
30 fps, ~16 ms decode, 0 drops.

## Codec verification

`vmx-go` has byte-for-byte golden tests against Rust-generated bitstreams
(64×64, 128×72, 320×180) and compares decoded UYVY output with the Rust
decoder. A 1920×1080 synthetic frame was also verified byte-identical during
development.
