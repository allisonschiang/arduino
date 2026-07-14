package unoq

import (
	"arduino/utils"
	"context"
	"fmt"
	"sync"
)

// pwmPins is the set of Arduino UNO Q pins that support PWM, per the board
// devicetree (zephyr_user `pwms`). The firmware is the authority; this mirrors it
// so the module rejects obviously-invalid pins before a round-trip.
var pwmPins = map[string]bool{
	"2": true, "3": true, "5": true, "6": true, "7": true, "8": true, "9": true,
	"10": true, "11": true, "12": true, "13": true, "20": true, "21": true,
}

// gpioPin implements board.GPIOPin for a single Arduino digital pin.
//
// The STM32 firmware cannot read back PWM duty/frequency, so the last values set
// are cached here and returned from PWM()/PWMFreq().
type gpioPin struct {
	pinNum string
	serial sender

	mu   sync.Mutex
	duty float64
	freq uint
}

// Get returns the current high/low state of the pin.
func (p *gpioPin) Get(ctx context.Context, _ map[string]interface{}) (bool, error) {
	pin, err := utils.PinToInt(p.pinNum)
	if err != nil {
		return false, err
	}
	res, err := p.serial.call(ctx, "gpio_get", pin)
	if err != nil {
		return false, err
	}
	val, ok := utils.ToBool(res)
	if !ok {
		return false, fmt.Errorf("gpio_get: unexpected result %v", res)
	}
	return val, nil
}

// Set drives the pin high or low.
func (p *gpioPin) Set(ctx context.Context, high bool, _ map[string]interface{}) error {
	pin, err := utils.PinToInt(p.pinNum)
	if err != nil {
		return err
	}
	_, err = p.serial.call(ctx, "gpio_set", pin, high)
	return err
}

// PWM returns the last duty cycle set (0.0–1.0). The firmware cannot read it back.
func (p *gpioPin) PWM(_ context.Context, _ map[string]interface{}) (float64, error) {
	if !pwmPins[p.pinNum] {
		return 0, fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.duty, nil
}

// SetPWM sets the duty cycle (0.0–1.0).
func (p *gpioPin) SetPWM(ctx context.Context, dutyCyclePct float64, _ map[string]interface{}) error {
	if !pwmPins[p.pinNum] {
		return fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	pin, err := utils.PinToInt(p.pinNum)
	if err != nil {
		return err
	}
	res, err := p.serial.call(ctx, "pwm_set", pin, dutyCyclePct)
	if err != nil {
		return err
	}
	if ok, _ := utils.ToBool(res); !ok {
		return fmt.Errorf("firmware rejected pwm_set on pin %s", p.pinNum)
	}
	p.mu.Lock()
	p.duty = dutyCyclePct
	p.mu.Unlock()
	return nil
}

// PWMFreq returns the last frequency set in Hz. The firmware cannot read it back.
func (p *gpioPin) PWMFreq(_ context.Context, _ map[string]interface{}) (uint, error) {
	if !pwmPins[p.pinNum] {
		return 0, fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.freq, nil
}

// SetPWMFreq sets the PWM frequency in Hz.
func (p *gpioPin) SetPWMFreq(ctx context.Context, freqHz uint, _ map[string]interface{}) error {
	if !pwmPins[p.pinNum] {
		return fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	pin, err := utils.PinToInt(p.pinNum)
	if err != nil {
		return err
	}
	res, err := p.serial.call(ctx, "pwm_freq", pin, int(freqHz))
	if err != nil {
		return err
	}
	if ok, _ := utils.ToBool(res); !ok {
		return fmt.Errorf("firmware rejected pwm_freq on pin %s", p.pinNum)
	}
	p.mu.Lock()
	p.freq = freqHz
	p.mu.Unlock()
	return nil
}
