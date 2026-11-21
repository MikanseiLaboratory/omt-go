# Windows validation checklist

1. Install Go 1.27.x and set the SIMD experiment:

   ```powershell
   $env:GOEXPERIMENT = "simd"
   go version
   ```

2. Build/run the example:

   ```powershell
   go run ./examples/gio-monitor -mode monitor -v
   go run ./examples/gio-monitor -mode send -name "Go Test Patterns" -v
   ```

3. Verify discovery and direct URL:

   - `Go Test Patterns` appears in Studio Monitor's source list.
   - `Test Patterns` appears in the Go monitor's source list.
   - If discovery fails, connect with `-url omt://<ip>:<port>/<name>`.

4. Windows firewall may prompt on first listen; allow private networks.

5. Record:

   - `go version` and `GOEXPERIMENT`
   - selected GPU / D3D11 adapter
   - source list contents
   - resolution, fps, decode ms, drop count
   - any output from the terminal

## Validation results

Windows 11 (amd64, Go 1.27.0, `GOEXPERIMENT=simd`), 2026-11-21:

- `go test . ./cmd/...` passes with and without `GOEXPERIMENT=simd`; `go vet`
  is clean.
- Go test-pattern sender → Rust `receive_to_jpeg` (320×180, 30 fps): decoded
  frames, 0 drops.
- Gio example sender (`-width 640 -height 360 -port 6550`) → Rust
  `receive_to_jpeg`: decoded 640×360 frames, 0 drops.
- Gio example monitor connected to the Go CLI sender: subscribed and ran its
  decode loop without errors.
- Rust `send_colorbar` → Go receiver (320×180): VMX1 decoded, `OMTInfo` parsed.
- DNS-SD: Rust `Discovery` sees the Go sender, and the Go browser sees Rust
  sources. The Go sender advertises its SRV target as `HOST-omt.local` so it
  does not collide with another mDNS responder that already owns
  `HOSTNAME.local` on the same machine.
- Discovery server, both directions: Go server ↔ Rust client and Rust server ↔
  Go client register and relay sources.
- Discovery-server URLs bracket IPv6 literals (`omt://[::1]:6400/...`).

## Known limitations

- The monitor does not play audio.
- No automatic reconnect after a live socket failure.
- Alpha and preview receive are not implemented in v0.1.
