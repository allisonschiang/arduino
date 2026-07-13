#!/bin/bash
# setup.sh — first-run setup for viam:arduino:uno-q on the Arduino UNO Q.
# Viam runs this once when the module is installed.
#
# The module talks to the STM32 through the arduino-router MessagePack-RPC bridge,
# NOT a raw serial port. Arduino removed raw-serial access to the Linux side in
# ArduinoCore-zephyr >= 0.55.0 (see DESIGN.md). So this script:
#   1. Ensures arduino-router is running + enabled — the module is a CLIENT of it.
#      (The old module disabled the router; that was for the dead raw-serial path.)
#   2. Best-effort installs arduino-cli + the arduino:zephyr core + the
#      Arduino_RouterBridge library, and flashes the RouterBridge sketch.
#
# ASSUMPTION: on-device flashing via arduino-cli works and the sketch registers
# its RPC methods on boot. Firmware may instead be flashed via Arduino App Lab;
# if so, the flash steps below are a harmless fallback. See DESIGN.md.

set -uo pipefail  # deliberately not -e: setup is best-effort, must never brick install

log() { echo "[viam:arduino setup] $*"; }

if [ "$(uname -s)" != "Linux" ]; then
    log "Not Linux — skipping UNO Q setup."
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SKETCH_DIR="$SCRIPT_DIR/firmware/uno-q-firmware"

# ---------------------------------------------------------------------------
# 1. arduino-router MUST be running — the module speaks RPC through it.
# ---------------------------------------------------------------------------
log "Ensuring arduino-router is running and enabled..."
systemctl enable arduino-router 2>/dev/null || true
systemctl start  arduino-router 2>/dev/null || true

# ---------------------------------------------------------------------------
# 2. Best-effort firmware flash. If arduino-cli isn't present, assume the
#    firmware was flashed another way (e.g. Arduino App Lab) and continue.
# ---------------------------------------------------------------------------
ARDUINO_CLI="$(command -v arduino-cli || true)"
if [ -z "$ARDUINO_CLI" ]; then
    log "arduino-cli not found — skipping auto-flash."
    log "If the firmware is not already on the STM32, flash $SKETCH_DIR"
    log "(model Arduino UNO Q) via Arduino App Lab or arduino-cli."
    exit 0
fi

if ! "$ARDUINO_CLI" core list 2>/dev/null | grep -q "arduino:zephyr"; then
    log "Installing arduino:zephyr core..."
    "$ARDUINO_CLI" core update-index >/dev/null 2>&1 || true
    "$ARDUINO_CLI" core install arduino:zephyr >/dev/null 2>&1 || log "WARN: core install failed"
fi

# The RouterBridge sketch requires the Arduino_RouterBridge library.
"$ARDUINO_CLI" lib install Arduino_RouterBridge >/dev/null 2>&1 || log "WARN: Arduino_RouterBridge lib install failed"

# Detect the UNO Q FQBN; fall back to the well-known value.
FQBN="$("$ARDUINO_CLI" board listall 2>/dev/null | grep -iE 'uno.?q' | grep -o 'arduino:zephyr:[^ ]*' | head -1)"
[ -z "$FQBN" ] && FQBN="arduino:zephyr:unoq"
log "Using FQBN: $FQBN"

if [ -d "$SKETCH_DIR" ]; then
    log "Compiling firmware..."
    if "$ARDUINO_CLI" compile --fqbn="$FQBN" "$SKETCH_DIR" >/dev/null 2>&1; then
        log "Uploading firmware..."
        if "$ARDUINO_CLI" upload --fqbn="$FQBN" "$SKETCH_DIR" >/dev/null 2>&1; then
            log "Firmware flashed."
        else
            log "WARN: upload failed — flash manually or via Arduino App Lab."
        fi
    else
        log "WARN: compile failed — flash manually or via Arduino App Lab."
    fi
fi

log "Setup complete. The module connects to the STM32 via arduino-router."
