# CLAUDE.md — Arduino UNO Q board module

Orientation + decisions + gotchas for anyone (human or agent) picking up this repo.
For the RPC contract and the full architecture writeup, see `DESIGN.md`; for user-
facing config, see `README.md`; for open scoping questions, see `SCOPE.md`.

## What this is

A Viam `board` module (`rdk:component:board`, model `viam:arduino:uno-q`) for the
**Arduino UNO Q**. It runs on the UNO Q's Qualcomm Linux SoC and controls the
onboard **STM32U585** coprocessor's GPIO / analog / PWM / interrupts.

The UNO Q is two chips: a Qualcomm SoC running Debian + viam-server, and an STM32
running a Zephyr-based Arduino sketch. They talk over an internal UART.

## The core constraint (read this first)

You **cannot** open the STM32's serial port from Linux and speak Firmata/raw bytes.
Since ArduinoCore-zephyr ≥ 0.55.0, Arduino's **`arduino-router`** service owns the
internal link (`/dev/ttyHS1`) exclusively. So the module is a **client of
arduino-router**, speaking **MessagePack-RPC** over its Unix socket
(`/var/run/arduino-router.sock`). A companion firmware sketch
(`firmware/uno-q-firmware/`) uses the `Arduino_RouterBridge` library to register
the RPC methods the module calls. Don't try to disable arduino-router — go through it.

This is why the earlier firmata-over-serial approach was abandoned. Full root-cause
in `DESIGN.md`.

## Layout

```
unoq/     board implementation + unit tests (package unoq)
  module.go          board.Board impl, Config, construction, tick dispatch
  gpio.go            GPIOPin (Set/Get/PWM/SetPWM/PWMFreq/SetPWMFreq)
  analog.go          Analog reader (ADC)
  digitalinterrupt.go DigitalInterrupt (edge counter)
  rpc.go             in-house MessagePack-RPC client over the router socket
utils/    board-agnostic helpers (package utils)
  pin.go             PinToInt
  coerce.go          msgpack value coercion (ToInt/ToBool/ToUint32/…)
cmd/
  module/            module entrypoint (meta.json points here)
  cli/               on-chip hardware harness — drives NewUnoQ directly, no viam-server
  probe/             router-level smoke test — raw msgpack, bypasses the module
firmware/uno-q-firmware/   the STM32 sketch (.ino)
examples/client/           Go SDK full-stack test (needs a running machine)
test_board.py              Python SDK full-stack test
```

The firmware and the Go module are a **matched pair**: same RPC method names, the
`"UNO-Q v2"` version handshake (`firmwareVersion` in `module.go` must equal
`FIRMWARE_VERSION` in the `.ino`), and the tick protocol. Change one, change both.

## Architecture / data flow

```
viam-server ── gRPC ──> module (cmd/module) ── msgpack-RPC over unix socket ──>
  arduino-router ── internal UART ──> STM32 firmware ── GPIO/PWM/ADC/interrupts
```

- Method calls (gpio_set, adc_read, pwm_set, …): module → router → firmware, reply back.
- Interrupt edges: firmware pushes `Bridge.notify("tick", …)` → router → module,
  fanned out to `StreamTicks` subscribers and counted per `DigitalInterrupt`.

## Key decisions

- **In-house msgpack-RPC client, not an imported dep.** `arduino-router` is a
  service repo (heavy deps), not a library. `rpc.go` implements a small client on
  `github.com/vmihailenco/msgpack/v5` (same lib the router uses).
- **`Config` lives in `unoq`, not `utils`.** Single model, so config belongs with
  the component. `utils` is only the pure, board-agnostic helpers.
- **`resource.AlwaysRebuild`.** Any config change tears down and rebuilds; the
  router connection reopens from scratch, so a clean rebuild beats in-place reconfigure.
- **PWM read-back is cached module-side** — the STM32 can't report duty/freq back.
- **`SetPowerMode` / `DoCommand` return `UnimplementedError`** — intentional (no
  Viam power-mode mapping on a two-chip board; no custom commands). `Analog.Write`
  drives the STM32 DAC on A0/A1 only (channels 0/1); A2–A5 have no DAC and reject.

## Hardware gotchas (hard-won — don't rediscover these)

1. **arduino-router routing is point-to-point, NOT broadcast.** A notification goes
   only to the client that `$/register`ed that method name. The module MUST
   `$/register "tick"` on connect (`registerTick` in `module.go`), or interrupts are
   silently dead. If you run `cmd/cli` while viam-server is up, only one of them gets
   ticks — stop viam-agent first (see below).
2. **PWM devices are `zephyr,deferred-init`.** The pin mux lives in the `arduino`
   pinctrl state, not `default`. `pwm_is_ready_dt` reports not-ready until the core's
   `analogWrite` initializes the device. So the firmware routes via `analogWrite`
   **before** checking readiness / calling `pwm_set_dt`. Reverse the order and the
   first PWM call after a cold boot fails.
3. **Minimum PWM frequency is bounded per timer** (fixed `st,prescaler` in the board
   overlay). TIM1/TIM8 pins (5,7,11,12,13) reach ~38 Hz; most others floor ~488 Hz;
   2/20/21 are on a 32-bit timer (no real floor). For low-freq PWM use pin 5. This is
   a hardware limit, not a bug — the module doesn't clamp.
4. **The interrupt tick pipeline saturates ~800 edges/s.** Fine for buttons/encoders,
   but don't use edge-counting through the module to *measure* high-frequency signals —
   it caps out. (During dev I measured PWM freq on-MCU instead.)

## Build / test / package

```bash
make test              # go test ./... (both packages, mock-based, no hardware)
make lint              # gofmt + go vet
make module            # test + static linux/arm64 binary + bin/module.tar.gz
viam module build local        # runs the meta.json build the way the registry does
viam module update-models --binary bin/arduino   # confirms the model registers
```

Note: `viam module build local` on a Mac produces a **darwin** binary (VIAM_BUILD_OS/
ARCH unset). That tarball is NOT deployable to the board — the real linux/arm64
artifact comes from the cloud build (`viam module build start`) or `publish.yml`.

## Testing on real hardware

The board is reached over `adb`. Rough loop (needs the board's sudo password and
Viam machine creds — NOT stored in this repo):

- **Reflash firmware:** push the `.ino` to the chip, `arduino-cli compile --fqbn
  arduino:zephyr:unoq .` then `arduino-cli upload …`. (A "verify failed" on the
  secondary flash bank is a benign quirk of this dual-bank part.)
- **Run the harness:** `cmd/cli` builds against `unoq` and drives `NewUnoQ` directly
  against the router — no viam-server, no cloud. Cross-build for arm64, push, run.
- **Free the tick route first:** `systemctl stop viam-agent` so the harness can
  `$/register "tick"` (otherwise viam-server's module owns it). Restart viam-agent
  after.

The standard jumper rig for the 5-goal harness: `D9→A0` (analog + PWM duty),
`D5→D2` (PWM frequency + interrupts), `D13→D3` (digital loopback).

## What's verified vs NOT (be honest about this)

**Verified on hardware, accurately:** all five features — digital IO, analog,
PWM duty, PWM frequency, interrupts — through the real module code (`NewUnoQ` →
gpio/analog/rpc → router → firmware → hardware), with exact measurements.

**NOT yet verified:** the full Viam stack — SDK/app → viam-server → module **gRPC**
→ hardware. `cmd/cli` builds `Config` as a struct literal, so viam-server JSON config
parsing and `Validate()` in the resource graph, plus `StreamTicks` over a real gRPC
stream, are untested. Low-risk (standard Viam machinery) but real gaps. Close them
with `examples/client` (Go) or `test_board.py` (Python) against a running machine.

Also: whatever module is currently deployed under viam-server on the chip may be an
**older build** — redeploy the current code before any full-stack test.

## Remaining work to become an official module

Decisions (need the team):
- Org/ownership (viam-modules vs viam-labs) → fixes `module_id` and `visibility: public`.
- Firmware flash story: `setup.sh` best-effort flashes via `arduino-cli`; decide the
  reliable install path (auto-flash on first_run vs document Arduino App Lab).

Mechanical (once decided): register in registry, cut an `x.y.z-rcN` (publish.yml +
build-action + org key secrets), deploy-from-registry smoke test on real hardware,
branch protection + a Go-1.25-compatible golangci-lint in CI, then flip to `public`
and tag the real release.

## Conventions

Match the existing style (and the `raspberry-pi` module's): comments describe
**current behavior**, present tense. No "replaces X" / "old approach" / decision-
history breadcrumbs in code — this file and `DESIGN.md` hold the "why." Keep it
readable for someone setting the board up for the first time.
