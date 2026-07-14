package unoq

import (
	"arduino/utils"
	"context"
	"fmt"

	"go.viam.com/rdk/components/board"
)

const (
	adcMax      = 4095.0
	adcVoltage  = 3.3
	adcStepSize = adcVoltage / adcMax
)

// analogPin implements board.Analog for one of A0–A5.
type analogPin struct {
	channel string // "0" through "5"
	serial  sender
}

// Read calls adc_read and converts the raw 12-bit result to AnalogValue.
func (a *analogPin) Read(ctx context.Context, _ map[string]interface{}) (board.AnalogValue, error) {
	channel, err := utils.PinToInt(a.channel)
	if err != nil {
		return board.AnalogValue{}, fmt.Errorf("invalid analog channel %q: %w", a.channel, err)
	}
	res, err := a.serial.call(ctx, "adc_read", channel)
	if err != nil {
		return board.AnalogValue{}, err
	}
	value, ok := utils.ToInt(res)
	if !ok {
		return board.AnalogValue{}, fmt.Errorf("adc_read: unexpected result %v", res)
	}
	if value < 0 {
		return board.AnalogValue{}, fmt.Errorf("adc_read: firmware rejected channel %s", a.channel)
	}
	return board.AnalogValue{
		Value:    value,
		Min:      0,
		Max:      adcVoltage,
		StepSize: adcStepSize,
	}, nil
}

// Write drives the STM32 DAC on this channel. Only A0 and A1 (channels 0 and 1)
// have a DAC; value is 0–4095 (12-bit, 0–3.3V). Writing takes the pin over from
// its ADC input, so a channel can Read or Write but not both at once.
func (a *analogPin) Write(ctx context.Context, value int, _ map[string]interface{}) error {
	channel, err := utils.PinToInt(a.channel)
	if err != nil {
		return fmt.Errorf("invalid analog channel %q: %w", a.channel, err)
	}
	if channel != 0 && channel != 1 {
		return fmt.Errorf("analog write (DAC) only supported on A0 and A1 (channels 0-1), not channel %s", a.channel)
	}
	if value < 0 || value > int(adcMax) {
		return fmt.Errorf("analog write value %d out of range (0-4095)", value)
	}
	res, err := a.serial.call(ctx, "dac_write", channel, value)
	if err != nil {
		return err
	}
	if ok, _ := utils.ToBool(res); !ok {
		return fmt.Errorf("dac_write: firmware rejected channel %s", a.channel)
	}
	return nil
}
