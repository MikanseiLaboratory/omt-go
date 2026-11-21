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

## Known limitations

- The monitor does not play audio.
- No automatic reconnect after a live socket failure.
- Alpha and preview receive are not implemented in v0.1.
