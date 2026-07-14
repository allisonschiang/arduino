# CLAUDE.md — Arduino UNO Q board module

Orientation, design, and hardware gotchas for anyone (human or agent) working on
this repo. User-facing setup/config lives in `README.md`; this file is the
contributor/maintainer reference.

## What this is

A Viam `board` module (`rdk:component:board`, model `viam:arduino:uno-q`) for the
**Arduino UNO Q**. It runs on the UNO Q's Qualcomm Linux SoC and controls the
onboard **STM32U585** coprocessor's GPIO / analog / PWM / DAC / interrupts.

The UNO Q is two chips: a Qualcomm SoC running Debian + viam-server, and an STM32
running a Zephyr-based Arduino sketch. They talk over an internal UART.

## The core constraint (read this first)

You **cannot** open the STM32's serial port from Linux and talk to it directly.
Since ArduinoCore-zephyr ≥ 0.55.0, Arduino's **`arduino-router`** service owns the
internal link (`/dev/ttyHS1`) exclusively. So the module is a **client of
arduino-router**, speaking **MessagePack-RPC** over its Unix socket
(`/var/run/arduino-router.sock`). A companion firmware sketch
(`firmware/uno-q-firmware/`) uses the `Arduino_RouterBridge` library to register
the RPC methods the module calls. Don't try to disable arduino-router — go through it.

Why the serial port is off-limits — in ArduinoCore-zephyr **0.55.0** (PR #370):
- `Serial` was rerouted to the RouterBridge, not a raw UART.
- `Serial1` was remapped to the physical header pins (D0/D1).
- `Arduino_RouterBridge` became mandatory for Serial (raw `Serial` now `#error`s).

Arduino's UNO Q User Manual says it directly: *"Do not attempt to open
`/dev/ttyHS1` (on Linux) or `Serial1` (on Arduino/Zephyr) in your own code. These
interfaces are exclusively locked by the `arduino-router` service…"*

References:
- Core 0.55.0: https://github.com/arduino/ArduinoCore-zephyr/releases/tag/0.55.0 (PR #370)
- UNO Q User Manual: https://docs.arduino.cc/tutorials/uno-q/user-manual/
- Arduino_RouterBridge: https://github.com/arduino-libraries/Arduino_RouterBridge
- arduino-router: https://github.com/arduino/arduino-router

## Architecture / data flow

```
viam-server ── gRPC ──> module (cmd/module) ── msgpack-RPC over unix socket ──>
  arduino-router ── internal UART ──> STM32 firmware ── GPIO/PWM/ADC/DAC/interrupts
```

- Method calls (`gpio_set`, `adc_read`, `pwm_set`, …): module → router → firmware, reply back.
- Interrupt edges: firmware pushes `Bridge.notify("tick", …)` → router → module,
  fanned out to `StreamTicks` subscribers and counted per `DigitalInterrupt`.

The module is a router *client*: `setup.sh` ensures `arduino-router` is running and
must never disable it.

## Layout

```
unoq/     board implementation + unit tests (package unoq)
  module.go          board.Board impl, Config, construction, tick dispatch
  gpio.go            GPIOPin (Set/Get/PWM/SetPWM/PWMFreq/SetPWMFreq)
  analog.go          Analog reader (ADC) + writer (DAC)
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
tests/    hardware external test (package unoq_test, `hardware` build tag)
examples/client/           Go SDK full-stack test (needs a running machine)
examples/dac/              Go SDK DAC test (A0 -> A2 loopback)
test_board.py              Python SDK full-stack test
```

The firmware and the Go module are a **matched pair**: same RPC method names, the
`"UNO-Q v2"` version handshake (`firmwareVersion` in `module.go` must equal
`FIRMWARE_VERSION` in the `.ino`), and the tick protocol. Change one, change both.

## RPC method contract (firmware ↔ module)

Request/response methods the sketch registers via `Bridge.provide` / `provide_safe`:

| Method       | Args                     | Returns        | Notes |
|--------------|--------------------------|----------------|-------|
| `hello`      | –                        | string version | version handshake on connect |
| `gpio_set`   | `pin int, high bool`     | –              | `pinMode(OUTPUT)` + `digitalWrite` |
| `gpio_get`   | `pin int`                | `bool`         | `pinMode(INPUT)` + `digitalRead` |
| `pwm_set`    | `pin int, duty float`    | `bool`         | PWM pins (2,3,5,6,7,8,9,10,11,12,13,20,21) via `pwm_set_dt` |
| `pwm_freq`   | `pin int, hz int`        | `bool`         | period via `pwm_set_dt`; range bounded per-timer (see gotchas) |
| `adc_read`   | `channel int (0-5)`      | `int (0-4095)` | 12-bit ADC |
| `dac_write`  | `channel int (0-1), value int (0-4095)` | `bool` | 12-bit DAC on A0/A1 only |
| `int_config` | `pin int, mode string`   | `bool`         | RISING/FALLING/CHANGE/NONE |

Notification pushed via `Bridge.notify`:

| Notification | Params                          | Notes |
|--------------|---------------------------------|-------|
| `tick`       | `pin int, high bool, micros u64`| one per interrupt edge |

## Key decisions

- **In-house msgpack-RPC client, not an imported dep.** `arduino-router` is a
  service repo (cobra CLI, serial-discovery, 50+ transitive deps), not a library.
  `rpc.go` implements a small client on `github.com/vmihailenco/msgpack/v5` — the
  same serialization lib the router uses — so the wire format matches, no heavy dep.
- **`Config` lives in `unoq`, not `utils`.** Single model, so config belongs with
  the component. `utils` is only the pure, board-agnostic helpers.
- **`resource.AlwaysRebuild`.** Any config change tears down and rebuilds; the
  router connection reopens from scratch, so a clean rebuild beats in-place reconfigure.
- **PWM read-back is cached module-side** — the STM32 can't report duty/freq back,
  so `PWM()`/`PWMFreq()` return the last set value.
- **`SetPowerMode` / `DoCommand` return `UnimplementedError`** — intentional (no
  coherent Viam power-mode mapping on a two-chip board; no custom commands).
- **`Analog.Write` drives the STM32 DAC** on A0/A1 only (channels 0/1), value
  0–4095 (12-bit, 0–3.3 V); A2–A5 have no DAC and reject.

## Socket access

`/var/run/arduino-router.sock` is created by the router's systemd unit, mode 0666,
no auth. A non-Arduino client may call peer-registered methods with no
`$/setMaxMsgSize` / `$/register` handshake (`$/register` is only needed to
*receive* notifications). Confirmed on hardware.

## Hardware gotchas (hard-won — don't rediscover these)

1. **arduino-router routing is point-to-point, NOT broadcast.** A notification goes
   only to the client that `$/register`ed that method name. The module MUST
   `$/register "tick"` on connect (`registerTick` in `module.go`), or interrupts are
   silently dead. Regression-tested in `interrupt_integration_test.go`. If you run
   `cmd/cli` while viam-server is up, only one of them gets ticks — stop viam-agent first.
2. **PWM devices are `zephyr,deferred-init`.** The pin mux lives in the `arduino`
   pinctrl state, not `default`. `pwm_is_ready_dt` reports not-ready until the core's
   `analogWrite` initializes the device. So the firmware routes via `analogWrite`
   **before** checking readiness / calling `pwm_set_dt`. Reverse the order and the
   first PWM call after a cold boot fails. (`analogWrite` and `pwm_set_dt` share the
   same `zephyr_user pwms` spec, so duty + frequency compose; `pwm_set_dt` runs last.)
3. **Minimum PWM frequency is bounded per timer** (fixed `st,prescaler` in the board
   overlay, `timer_clk / (prescaler+1) / 65536` for 16-bit timers):
   - TIM4 (D9/D10), TIM3 (D3/D6/D8): prescaler 4 → ~32 MHz → **min ≈ 488 Hz**.
     TIM2 (D2/D20/D21) shares it but is 32-bit → effectively no floor.
   - TIM1 (D5/D11/D12/D13), TIM8 (D7): prescaler 63 → ~2.5 MHz → **min ≈ 38 Hz**.
   For low-freq PWM use a TIM1/TIM8 pin (e.g. D5). Hardware limit, not a bug — the
   module doesn't clamp; the driver sets what the timer supports.
4. **The interrupt tick pipeline saturates ~800 edges/s.** Fine for buttons/encoders,
   but don't count edges *through the module* to measure high-frequency signals — it
   caps out. To measure a high PWM frequency, use a lower rate or an on-MCU counter.

## Build / test / package

```bash
make test              # go test ./... (mock-based, no hardware)
make lint              # gofmt + go vet
make module            # test + static linux/arm64 binary + bin/module.tar.gz
viam module build local        # runs the meta.json build the way the registry does
viam module update-models --binary bin/arduino   # confirms the model registers
go test -tags hardware ./tests/... -v            # on-board hardware test (needs the rig)
```

Note: `viam module build local` on a Mac produces a **darwin** binary (VIAM_BUILD_OS/
ARCH unset). That tarball is NOT deployable to the board — the real linux/arm64
artifact comes from the cloud build (`viam module build start`) or `publish.yml`.

## Testing on real hardware

The board is reached over `adb`. Rough loop (needs the board's sudo password and
Viam machine creds — NOT stored in this repo):

- **Reflash firmware:** push the `.ino` to the chip, `arduino-cli compile
  --fqbn=arduino:zephyr:unoq .` then `arduino-cli upload …`. A "verify failed" on the
  secondary flash bank is a benign quirk of this dual-bank part.
- **Two ways to exercise it:**
  - `cmd/cli` drives `NewUnoQ` directly against the router — no viam-server. Fast, but
    stop viam-agent first (`systemctl stop viam-agent`) so the harness owns the tick route.
  - `examples/client` / `examples/dac` (Go SDK) or `test_board.py` (Python) go through
    the full stack: SDK → viam-server → module gRPC → hardware. Needs the module
    deployed + machine creds.

Standard jumper rig: `D9→A0` (analog + PWM duty), `D5→D2` (PWM frequency + interrupts),
`D13→D3` (digital loopback). DAC test uses `A0→A2` instead of `D9→A0`.

## What's verified

All five goals **plus** `Analog.Write` (DAC) are verified on hardware, both ways —
through the module code directly (`cmd/cli`) and through the **full Viam stack**
(Go SDK → viam-server → module gRPC → firmware → hardware, via `examples/client` /
`examples/dac`). That covers JSON config parsing (`analogs`/`digital_interrupts` from
cloud config), `Validate`, module gRPC registration + construction, method calls,
`StreamTicks`, and the DAC.

One measurement caveat (not a correctness gap): counting interrupt edges *through the
module* to measure absolute PWM **frequency** is noisy — the tick pipeline caps
~800 edges/s and adds gRPC jitter. Frequency accuracy was confirmed separately by
on-MCU counting (exact 50/100/200 Hz). Everything else reads back exact.

## Remaining work to become an official module

Decisions (need the team):
- Org/ownership (viam-modules vs viam-labs) → fixes `module_id` and `visibility: public`.

Mechanical (once decided): register in registry, cut an `x.y.z-rcN` (publish.yml +
build-action + org key secrets), deploy-from-registry smoke test on real hardware,
branch protection + a Go-1.25-compatible golangci-lint in CI, then flip to `public`
and tag the real release. (Firmware flashing is handled: `setup.sh` auto-flashes on
first run, with a manual Arduino App Lab / arduino-cli fallback documented in the README.)

## Conventions

Match the `raspberry-pi` module's style: comments describe **current behavior**,
present tense. No "replaces X" / "old approach" / decision-history breadcrumbs in
code — this file holds the "why." Keep code readable for someone setting the board
up for the first time.
