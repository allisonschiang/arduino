# Arduino UNO Q board module — design

## Why arduino-router, not a raw serial port

The obvious approach — open the STM32's serial port (`/dev/ttyHS1`) from the Linux
side and speak Firmata/ASCII — does **not** work on the UNO Q. In
`ArduinoCore-zephyr` **0.55.0** (PR #370,
*"zephyrSerial: provide ARDUINO_ROUTER_SERIAL from DTS"*):

- `Serial` was rerouted to the **Monitor/RouterBridge**, not a raw UART.
- `Serial1` was remapped to the **physical header pins (D0/D1)** — no longer the
  internal SoC link.
- The `Arduino_RouterBridge` library became **mandatory** for Serial support
  (sketches that use raw `Serial` now fail to compile with a `#error`).

Arduino's UNO Q User Manual states it directly:

> "Do not attempt to open `/dev/ttyHS1` (on Linux) or `Serial1` (on Arduino/Zephyr)
> in your own code. These interfaces are exclusively locked by the `arduino-router`
> service, and attempting to access them directly will cause the Bridge to fail."

So on any UNO Q at core ≥ 0.55.0, a raw-serial approach:
1. won't compile (RouterBridge `#error`),
2. can't open the port (`arduino-router` holds it exclusively), and
3. wouldn't carry raw bytes anyway (the link speaks MessagePack-RPC).

**Conclusion:** the supported path is to speak the RouterBridge RPC — go *with*
the router as a client instead of trying to bypass it.

### Sources
- Release 0.55.0: https://github.com/arduino/ArduinoCore-zephyr/releases/tag/0.55.0
- PR #370: https://github.com/arduino/ArduinoCore-zephyr/pull/370
- UNO Q User Manual: https://docs.arduino.cc/tutorials/uno-q/user-manual/
- Arduino_RouterBridge: https://github.com/arduino-libraries/Arduino_RouterBridge
- arduino-router (service + Go msgpackrpc client): https://github.com/arduino/arduino-router

## Architecture

```
viam-server (Qualcomm Linux)
  └── viam:arduino:uno-q module (Go)
        │  MessagePack-RPC over /var/run/arduino-router.sock
        ▼
     arduino-router (Golang service — the supported bridge)
        │  internal UART
        ▼
     STM32U585  (uno-q-firmware.ino using Arduino_RouterBridge)
        └── Bridge.provide(...) handlers → digitalWrite / analogRead / analogWrite / attachInterrupt
        └── Bridge.notify("tick", ...) → interrupt edges pushed to the host
```

The module is a router *client*: `setup.sh` ensures `arduino-router` is running and
must never disable it.

## RPC method contract (firmware ↔ module)

Request/response methods the sketch registers via `Bridge.provide` / `provide_safe`:

| Method       | Args                     | Returns        | Notes |
|--------------|--------------------------|----------------|-------|
| `hello`      | –                        | string version | version handshake on connect |
| `gpio_set`   | `pin int, high bool`     | –              | `pinMode(OUTPUT)` + `digitalWrite` |
| `gpio_get`   | `pin int`                | `bool`         | `pinMode(INPUT)` + `digitalRead` |
| `pwm_set`    | `pin int, duty float`    | `bool`         | PWM pins (2,3,5,6,7,8,9,10,11,12,13,20,21) via `pwm_set_dt` |
| `pwm_freq`   | `pin int, hz int`        | `bool`         | period via `pwm_set_dt`; range bounded per-timer (see below) |
| `adc_read`   | `channel int (0-5)`      | `int (0-4095)` | 12-bit ADC |
| `int_config` | `pin int, mode string`   | –              | RISING/FALLING/CHANGE/NONE |

Notification the sketch pushes via `Bridge.notify`:

| Notification | Params                          | Notes |
|--------------|---------------------------------|-------|
| `tick`       | `pin int, high bool, micros u64`| one per interrupt edge |

PWM/PWMFreq *read-back*: the STM32 can't read them back, so the module caches the
last-set duty/frequency and returns from cache.

## Design decisions

- **In-house MessagePack-RPC client, not an imported dependency.**
  `github.com/arduino/arduino-router` is a service (cobra CLI, serial-discovery,
  50+ transitive deps), not meant to be imported as a library. The module
  implements a small client (`rpc.go`) on `github.com/vmihailenco/msgpack/v5` —
  the same serialization library arduino-router uses — so the wire format matches
  with no heavy dependency.
- **Notification routing is point-to-point.** arduino-router forwards a
  notification only to the client that `$/register`ed that method name; it does
  not broadcast. The module registers `"tick"` on connect (`registerTick`),
  otherwise interrupt edges never arrive. Regression-tested in
  `interrupt_integration_test.go`.
- **Config schema:** `router_socket` (default `/var/run/arduino-router.sock`),
  `analogs`, and `digital_interrupts`.
- **PWM read-back is cached module-side** — the STM32 can't report it.
- **`resource.AlwaysRebuild`:** a config change tears the board down and rebuilds
  it. The router connection is reopened from scratch anyway, so a clean rebuild is
  simpler and safer than an in-place reconfigure.

## Socket access

`/var/run/arduino-router.sock` is created by the router's systemd unit, mode 0666,
no auth. A non-Arduino client may call peer-registered methods with no
`$/setMaxMsgSize` / `$/register` handshake (`$/register` is only needed to
*receive* notifications). Confirmed on hardware.

## Verified on hardware

All five goals, exercised through the real `NewUnoQ` board code via `cmd/cli`
(against the router, no viam-server) on the jumper rig D9→A0, D5→D2, D13→D3:
- **Digital IO** — D13 set high/low read back exactly on D3.
- **Analog** — D9 high → A0 = 4095, low → 0.
- **PWM duty** — set 0.25/0.50/0.75 → measured 0.23/0.51/0.78 (ADC-averaged).
- **PWM frequency** — set 50/100/200 Hz on D5 → 50.0/100.0/201.5 Hz measured by
  counting D2 edges. Confirmed independently on-MCU (ISR edge count) as exact.
- **Interrupts** — int2 edge counts 100/200/403 ≈ 2× the set frequency (CHANGE
  fires on both edges), i.e. accurate, not just non-zero.

### PWM implementation notes

- **PWM devices are deferred-init.** The board overlay marks the PWM controller
  nodes `zephyr,deferred-init` with the real pin mux in the `arduino` pinctrl
  state (not `default`). `pwm_is_ready_dt` reports *not ready* until the device is
  initialized, which the core's `analogWrite` does (applying the `arduino` pinctrl
  state). So the firmware routes via `analogWrite` **before** checking readiness
  or calling `pwm_set_dt`; doing it the other way makes the first PWM call on a pin
  fail after a cold MCU boot.
- **`analogWrite` and `pwm_set_dt` share the same `zephyr_user pwms` spec** (same
  device + channel), so duty and frequency compose without conflict; `pwm_set_dt`
  runs last and wins.
- **Minimum frequency is bounded per timer.** Each timer's `st,prescaler` is fixed
  in the overlay, giving `timer_clk / (prescaler+1) / 65536` as the 16-bit floor:
  - TIM4 (D9/D10), TIM3 (D3/D6/D8): prescaler 4 → ~32 MHz → **min ≈ 488 Hz**.
    TIM2 (D2/D20/D21) shares that prescaler but is 32-bit → effectively no floor.
  - TIM1 (D5/D11/D12/D13), TIM8 (D7): prescaler 63 → ~2.5 MHz → **min ≈ 38 Hz**.
  For low-frequency PWM use a TIM1/TIM8 pin (e.g. D5). The module does not clamp;
  the driver sets what the timer supports.
