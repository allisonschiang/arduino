# `viam:arduino` — Arduino UNO Q board module

A [Viam module](https://docs.viam.com/registry/) that exposes the **Arduino UNO Q**'s
GPIO, analog inputs, PWM, and digital interrupts to a `viam-server` machine. It
implements the [`rdk:component:board`](https://docs.viam.com/components/board/) API.

The module runs on the UNO Q's **Qualcomm Linux SoC** and talks to the onboard
**STM32U585** coprocessor through Arduino's **`arduino-router`** service using
MessagePack-RPC. See [CLAUDE.md](CLAUDE.md) for why this is the supported path on
the UNO Q.

## Architecture

```
viam-server (Qualcomm Linux SoC)
  └── viam:arduino:uno-q module (Go)
        │  MessagePack-RPC over /var/run/arduino-router.sock
        ▼
     arduino-router (Arduino's bridge service)
        │  internal link
        ▼
     STM32U585  (firmware/uno-q-firmware — Arduino_RouterBridge sketch)
        └── GPIO / PWM / ADC / interrupts on the headers
```

The module is a **client of `arduino-router`** — the router must be running (it is
by default). The firmware registers RPC methods (`gpio_set`, `gpio_get`, `pwm_set`,
`pwm_freq`, `adc_read`, `int_config`) and pushes `tick` notifications on interrupt
edges.

## Models

| Model | Description |
|-------|-------------|
| `viam:arduino:uno-q` | Board component for the Arduino UNO Q |

## Requirements

- **Hardware:** Arduino UNO Q.
- **Firmware:** the RouterBridge sketch must be on the STM32 — see
  [Flashing the firmware](#flashing-the-firmware) below.
- **`arduino-router` service:** must be **running** (the default on the UNO Q).
  The module talks through it, so don't disable it.

## Flashing the firmware

The board's STM32 coprocessor must run the RouterBridge sketch
(`firmware/uno-q-firmware/uno-q-firmware.ino`) for the module to work.

**Automatic (default).** On first install Viam runs `setup.sh`, which best-effort
flashes the sketch with `arduino-cli` (installing the `arduino:zephyr` core and the
`Arduino_RouterBridge` library first). If it succeeds you don't need to do anything.

**Manual fallback (if auto-flash fails).** `setup.sh` is best-effort — if
`arduino-cli` isn't available or a step fails, flash it yourself. The sketch ships
inside the module package at `firmware/uno-q-firmware/uno-q-firmware.ino`.

*Option A — Arduino App Lab (easiest):*
1. Open **Arduino App Lab** on the UNO Q (or connect the board over USB-C).
2. Create/open a sketch and paste in the contents of `uno-q-firmware.ino`, or open
   the file directly.
3. Add the **Arduino_RouterBridge** library (Library Manager) if prompted.
4. Select the **Arduino UNO Q** board and click **Upload**.

*Option B — `arduino-cli` (on the board's Linux side):*
```bash
arduino-cli lib install Arduino_RouterBridge
arduino-cli compile --fqbn=arduino:zephyr:unoq firmware/uno-q-firmware
arduino-cli upload  --fqbn=arduino:zephyr:unoq firmware/uno-q-firmware
```
A "verify failed" warning on the second flash bank is a benign quirk of this
dual-bank STM32 — the upload still succeeds.

Confirmed flashed when the module connects without a handshake timeout (the
firmware answers the `hello` RPC with its version string).

## Configure your UNO Q board

Add a `viam:arduino:uno-q` board component to your machine and give it the
attributes below.

### Attributes

| Name | Type | Inclusion | Default | Description |
|------|------|-----------|---------|-------------|
| `router_socket` | string | Optional | `/var/run/arduino-router.sock` | Path to the `arduino-router` Unix socket. |
| `analogs` | array | Optional | `[]` | Analog input channels to expose by name (see below). |
| `digital_interrupts` | array | Optional | `[]` | Digital interrupt channels to expose by name (see below). |

`analogs[]` items: `name` (used with `AnalogByName`) and `pin` (`"0"`–`"5"` for A0–A5).

`digital_interrupts[]` items: `name` (used with `DigitalInterruptByName`), `pin`
(Arduino pin number, e.g. `"2"`), and `mode` (`"RISING"`, `"FALLING"`, or
`"CHANGE"` — default `"CHANGE"`).

### Example configuration

```json
{
  "analogs": [
    { "name": "joystick_x", "pin": "0" },
    { "name": "thermistor", "pin": "1" }
  ],
  "digital_interrupts": [
    { "name": "encoder-a", "pin": "2", "mode": "CHANGE" },
    { "name": "button",    "pin": "3", "mode": "RISING" }
  ]
}
```

A minimal config is `{}` — the router socket defaults, and no analog/interrupt
channels are exposed until you declare them.

## Capabilities

### Digital GPIO
Any digital pin by number. Pins are created lazily on first access.
```python
pin = await board.gpio_pin_by_name("13")
await pin.set(True)      # drive high (3.3 V)
high = await pin.get()   # read back
```

### PWM
Supported on pins **2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 20, 21**. Duty is 0.0–1.0.
```python
pin = await board.gpio_pin_by_name("9")
await pin.set_pwm(0.5)         # 50% duty
await pin.set_pwm_freq(1000)   # 1 kHz
```
`pwm()` / `pwm_freq()` return the **last value set** — the STM32 can't read them
back, so the module caches them.

The minimum settable frequency depends on the pin's timer (its prescaler is fixed
in the board firmware). Pins on TIM1/TIM8 (**5, 7, 11, 12, 13**) reach down to
~38 Hz; the others have a floor near ~488 Hz, except **2, 20, 21** (a 32-bit timer,
effectively no floor). For low-frequency PWM, use pin 5.

### Analog input
Channels A0–A5, declared in `analogs`, 12-bit (0–4095), 3.3 V reference.
```python
reader = await board.analog_by_name("joystick_x")
reading = await reader.read()   # reading.value in 0..4095
```

### Analog output (DAC)
`Analog.write()` drives a real analog voltage (the STM32 DAC), **only on A0 and A1**
(channels 0 and 1), value 0–4095 (12-bit, 0–3.3 V). A channel drives *or* reads —
using A0/A1 as a DAC takes the pin over from its ADC input.
```python
dac = await board.analog_by_name("a0")   # a0 configured on pin "0"
await dac.write(2048)                     # ~1.65 V out
```

### Digital interrupts
Declared in `digital_interrupts`. `Value()` returns the cumulative tick count;
`StreamTicks` streams edges.
```python
di = await board.digital_interrupt_by_name("encoder-a")
count = await di.value()
```

## Not supported

| Feature | Status |
|---------|--------|
| Analog write on A2–A5 | only A0/A1 have a DAC |
| `SetPowerMode` | not supported (returns unimplemented) |
| `pwm()` / `pwm_freq()` hardware read-back | cached module-side instead |

## Development

```bash
make setup            # go mod tidy
make test             # go test -race ./...  (mock RPC — no hardware needed)
make module           # static build + bin/module.tar.gz
```

The transport is behind the `sender` interface (`rpc.go`); tests inject a mock,
so the full board logic is verified without hardware. See [CLAUDE.md](CLAUDE.md)
for the architecture and the RPC method contract.
