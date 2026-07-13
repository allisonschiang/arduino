# Scope — Arduino UNO Q board module

Draft for team review (naming, attributes, architectures). See DESIGN.md for the
technical architecture (why RouterBridge RPC, not raw serial).

## Overview

A Viam `board` component for the **Arduino UNO Q**. Runs on the UNO Q's Qualcomm
Linux side and controls the STM32U585 coprocessor's GPIO / analog / PWM /
interrupts through Arduino's `arduino-router` MessagePack-RPC bridge (the
supported path since ArduinoCore-zephyr 0.55.0 removed raw serial). A companion
sketch (`firmware/uno-q-firmware`) using `Arduino_RouterBridge` runs on the MCU.

## Naming

- **Current:** `module_id: viam:arduino`, repo `viam-labs/arduino`, model
  `viam:arduino:uno-q`. Fine for v1 / iteration.
- **For official viam-modules:** convention is `viam:<github-repo-name>`. When
  moved to the `viam-modules` org, module_id becomes `viam:<repo>` and model
  `viam:<repo>:uno-q`. Low-effort rename — deferred. *(Team: confirm repo name.)*

## Model + attributes

### `viam:arduino:uno-q` (api `rdk:component:board`)

| Attribute | Type | Req | Default | Meaning |
|-----------|------|-----|---------|---------|
| `router_socket` | string | opt | `/var/run/arduino-router.sock` | arduino-router Unix socket the module dials. Configurable only for non-standard images. |
| `analogs` | array | opt | `[]` | Analog readers to expose. Each: `name` (used by `AnalogByName`) + `pin` (ADC channel `"0"`–`"5"` = A0–A5). |
| `digital_interrupts` | array | opt | `[]` | Interrupts to expose. Each: `name` (used by `DigitalInterruptByName`), `pin` (Arduino pin), `mode` (`RISING`/`FALLING`/`CHANGE`, default `CHANGE`). |

Name choices mirror the existing rdk board config schema (`analogs`,
`digital_interrupts`) so configs are familiar to Viam board users. `router_socket`
is module-specific (no raw `serial_path`/`baud_rate` — those belonged to the dead
raw-serial approach).

## meta.json

```json
{
  "$schema": "https://dl.viam.dev/module.schema.json",
  "module_id": "viam:arduino",
  "visibility": "public_unlisted",
  "url": "https://github.com/viam-labs/arduino",
  "description": "Viam board module for the Arduino UNO Q — GPIO, analog, PWM, and digital interrupts via the arduino-router bridge.",
  "models": [
    { "api": "rdk:component:board", "model": "viam:arduino:uno-q",
      "short_description": "...", "markdown_link": "README.md#configure-your-uno-q-board" }
  ],
  "markdown_link": "README.md",
  "entrypoint": "bin/arduino",
  "first_run": "setup.sh",
  "build": { "build": "make module", "path": "bin/module.tar.gz", "arch": ["linux/arm64"] }
}
```

## Architectures

- **Claimed: `linux/arm64` only.** The UNO Q's Linux side *is* arm64, and the
  module only runs on the board. There is no meaningful darwin/amd64 *runtime*
  target. *(Team question: does the official-module build-for-each-arch criterion
  require darwin/arm64 build artifacts for a hardware-specific arm64 board, or is
  linux/arm64-only acceptable? — raise with fleet/dx.)*

## Board API coverage (rdk v0.121)

Implemented: `GPIOPinByName` (Set/Get/PWM/SetPWM/PWMFreq/SetPWMFreq),
`AnalogByName` (Read), `DigitalInterruptByName`, `StreamTicks`, `Reconfigure`
(via `AlwaysRebuild`), `Close`, `Name`, `Status`. `SetPowerMode` and
`Analog.Write` return `UnimplementedError` (not supported by the hardware). This
is the full current board API surface (no I2C/SPI in this API version).

## v1 goals / tickets

- [x] RouterBridge RPC transport + firmware; GPIO/analog/PWM/interrupts working on hardware
- [x] Unit tests (`make test`), `make lint`, `make module` package rule
- [x] Apache-2.0 LICENSE, .gitignore, no tracked binaries
- [x] README (config, methods, triplets), DESIGN.md, this scope doc
- [x] CI: `test.yml` (lint+test), `publish.yml` (build-action on release)
- [ ] Move to `viam-modules` (or keep viam-labs) — team decision
- [ ] golangci-lint version compatible with Go 1.25 wired into CI
- [ ] Registry publish as `x.y.z-rcN`, deploy-from-registry test on linux/arm64
- [ ] Branch protection (PR + status checks)
- [ ] Hardware canary test (needs an UNO Q on a runner) — team feasibility

## Open questions for the team

1. Repo name / org (viam-labs vs viam-modules) → module_id.
2. Architectures: is linux/arm64-only acceptable for an arm64-only board?
3. Hardware-in-the-loop CI: feasible to attach an UNO Q to a runner?
4. Firmware distribution: `setup.sh` flashes via arduino-cli (best-effort) vs
   documenting Arduino App Lab as the flash path.
