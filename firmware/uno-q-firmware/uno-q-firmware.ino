// Viam Arduino UNO Q board firmware.
//
// Runs on the STM32U585 and communicates with the Linux-side Viam module through
// Arduino's arduino-router service using the Arduino_RouterBridge library
// (MessagePack-RPC). It registers the RPC methods the module calls and pushes
// "tick" notifications on interrupt edges.
//
// Install: Arduino_RouterBridge from the Library Manager. Do not disable
// arduino-router — this sketch talks through it.

#include <Arduino_RouterBridge.h>
#include <zephyr/drivers/pwm.h>

#define FIRMWARE_VERSION "UNO-Q v2"

// ---- Digital interrupt support ----
#define MAX_INT_SLOTS 8

struct IntSlot {
  int  pin;
  bool active;
  volatile uint32_t count;   // total edges seen (incremented in the ISR)
  volatile bool lastHigh;    // level at the most recent edge
  uint32_t emitted;          // edges already sent to the host (loop-only)
};

static IntSlot intSlots[MAX_INT_SLOTS];

// The ISR counts every edge so fast signals (e.g. encoders) never coalesce;
// loop() emits one "tick" per counted edge.
#define MAKE_ISR(N)                                   \
  static void isr##N() {                              \
    if (intSlots[N].active) {                         \
      intSlots[N].count++;                            \
      intSlots[N].lastHigh = (bool)digitalRead(intSlots[N].pin); \
    }                                                 \
  }
MAKE_ISR(0) MAKE_ISR(1) MAKE_ISR(2) MAKE_ISR(3)
MAKE_ISR(4) MAKE_ISR(5) MAKE_ISR(6) MAKE_ISR(7)

static voidFuncPtr isrTable[MAX_INT_SLOTS] =
    {isr0, isr1, isr2, isr3, isr4, isr5, isr6, isr7};

// PWM channels come straight from the board devicetree (zephyr_user `pwms`
// property), so the correct timer/channel + polarity per pin is handled by the
// Zephyr PWM driver — no hand-rolled timer-register pokes. Both duty and
// frequency go through pwm_set_dt(), which rescales them together.
#define PWM_SPEC(node, prop, idx) PWM_DT_SPEC_GET_BY_IDX(node, idx),
static const struct pwm_dt_spec pwmSpecs[] = {
  DT_FOREACH_PROP_ELEM(DT_PATH(zephyr_user), pwms, PWM_SPEC)};

// Arduino pin number for each spec above, in devicetree order (D-pins only; the
// trailing internal-LED channels are ignored).
//
// Minimum settable PWM frequency depends on the pin's timer, whose prescaler is
// fixed in the board overlay (st,prescaler), giving these 16-bit floors:
//   TIM1 (D5,D11,D12,D13), TIM8 (D7): prescaler 63 -> ~2.5 MHz -> min ~38 Hz
//   TIM3 (D3,D6,D8), TIM4 (D9,D10):   prescaler 4  -> ~32 MHz  -> min ~488 Hz
//   TIM2 (D2,D20,D21): prescaler 4, 32-bit counter -> effectively no floor
// For low-frequency PWM use a TIM1/TIM8 pin (e.g. D5). Verified on hardware.
static const int pwmPinNums[] = {2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 20, 21};
#define PWM_PIN_COUNT ((int)(sizeof(pwmPinNums) / sizeof(pwmPinNums[0])))

struct PwmState { uint32_t periodNs; float duty; };
static PwmState pwmState[PWM_PIN_COUNT];

static int pwmIndex(int pin) {
  for (int i = 0; i < PWM_PIN_COUNT; i++) {
    if (pwmPinNums[i] == pin) return i;
  }
  return -1;
}

// ---- RPC handlers ----
// Returned bool = success where the host cares; false signals a rejected request
// (e.g. non-PWM pin). digitalWrite/analogRead/analogWrite run here.

static String rpc_hello() {
  return String(FIRMWARE_VERSION);
}

static bool rpc_gpio_set(int pin, bool high) {
  pinMode(pin, OUTPUT);
  digitalWrite(pin, high ? HIGH : LOW);
  return true;
}

static bool rpc_gpio_get(int pin) {
  pinMode(pin, INPUT);
  return (bool)digitalRead(pin);
}

// Returns the raw 0-4095 12-bit reading, or -1 for an invalid channel.
static int rpc_adc_read(int channel) {
  if (channel < 0 || channel > 5) return -1;
  return analogRead(A0 + channel);
}

// rpc_dac_write drives the STM32 DAC. Only channels 0 and 1 (A0/A1) have a DAC;
// value is 0-4095 (12-bit, 0-3.3V). Using A0/A1 as a DAC output takes over the
// pin from its ADC input.
static bool rpc_dac_write(int channel, int value) {
  if (channel < 0 || channel > 1) return false;
  if (value < 0) value = 0;
  if (value > 4095) value = 4095;
  analogWriteResolution(12);
  analogWrite((dacPins)channel, value);
  return true;
}

static bool rpc_pwm_set(int pin, float duty) {
  int i = pwmIndex(pin);
  if (i < 0) return false;
  if (duty < 0.0f) duty = 0.0f;
  if (duty > 1.0f) duty = 1.0f;
  // analogWrite applies the per-channel pinctrl AND initializes the
  // devicetree deferred-init PWM device. Must run before pwm_is_ready_dt,
  // which reports not-ready until the device is initialized (e.g. after a
  // fresh MCU boot, before this pin has ever been driven).
  analogWrite(pin, (int)(duty * 255.0f));
  if (!pwm_is_ready_dt(&pwmSpecs[i])) return false;
  // Default to the devicetree period (500 Hz) until a frequency is set.
  uint32_t period = pwmState[i].periodNs ? pwmState[i].periodNs : pwmSpecs[i].period;
  if (pwm_set_dt(&pwmSpecs[i], period, (uint32_t)(duty * period)) != 0) return false;
  pwmState[i].duty = duty;
  if (pwmState[i].periodNs == 0) pwmState[i].periodNs = period;
  return true;
}

static bool rpc_pwm_freq(int pin, int freqHz) {
  int i = pwmIndex(pin);
  if (i < 0 || freqHz <= 0) return false;
  float duty = pwmState[i].duty;
  analogWrite(pin, (int)(duty * 255.0f)); // route + init deferred device
  if (!pwm_is_ready_dt(&pwmSpecs[i])) return false;
  uint32_t period = 1000000000UL / (uint32_t)freqHz; // nanoseconds
  if (pwm_set_dt(&pwmSpecs[i], period, (uint32_t)(duty * period)) != 0) return false;
  pwmState[i].periodNs = period;
  return true;
}

// mode: "RISING" | "FALLING" | "CHANGE" | "NONE"
static bool rpc_int_config(int pin, String mode) {
  mode.trim();

  // Detach any existing slot for this pin.
  for (int i = 0; i < MAX_INT_SLOTS; i++) {
    if (intSlots[i].active && intSlots[i].pin == pin) {
      detachInterrupt(digitalPinToInterrupt(pin));
      intSlots[i].active = false;
      break;
    }
  }

  if (mode == "NONE") return true;

  int slot = -1;
  for (int i = 0; i < MAX_INT_SLOTS; i++) {
    if (!intSlots[i].active) { slot = i; break; }
  }
  if (slot < 0) return false; // no free slots

  PinStatus imode = CHANGE;
  if (mode == "RISING")  imode = RISING;
  if (mode == "FALLING") imode = FALLING;

  pinMode(pin, INPUT);
  intSlots[slot] = { pin, true, 0, (bool)digitalRead(pin), 0 };
  attachInterrupt(digitalPinToInterrupt(pin), isrTable[slot], imode);
  return true;
}

void setup() {
  analogReadResolution(12); // 12-bit ADC: 0-4095

  Bridge.begin();

  // provide_safe runs each handler in loop() context (serviced automatically by
  // the core), so handlers safely share pins with the interrupt bookkeeping below.
  Bridge.provide_safe("hello",      rpc_hello);
  Bridge.provide_safe("gpio_set",   rpc_gpio_set);
  Bridge.provide_safe("gpio_get",   rpc_gpio_get);
  Bridge.provide_safe("adc_read",   rpc_adc_read);
  Bridge.provide_safe("dac_write",  rpc_dac_write);
  Bridge.provide_safe("pwm_set",    rpc_pwm_set);
  Bridge.provide_safe("pwm_freq",   rpc_pwm_freq);
  Bridge.provide_safe("int_config", rpc_int_config);
}

void loop() {
  // Emit one "tick" notification per counted interrupt edge, catching up if
  // several edges arrived between iterations (so counts are never lost).
  for (int i = 0; i < MAX_INT_SLOTS; i++) {
    if (!intSlots[i].active) continue;
    uint32_t c = intSlots[i].count; // snapshot the volatile counter
    while (intSlots[i].emitted < c) {
      intSlots[i].emitted++;
      Bridge.notify("tick", intSlots[i].pin, (int)intSlots[i].lastHigh, (uint32_t)micros());
    }
  }
}

// PWM frequency + duty are handled by the Zephyr PWM driver (pwm_set_dt) in the
// handlers above — no direct timer-register access.
