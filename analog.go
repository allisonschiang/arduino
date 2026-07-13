package arduino

import (
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
	channel, err := pinToInt(a.channel)
	if err != nil {
		return board.AnalogValue{}, fmt.Errorf("invalid analog channel %q: %w", a.channel, err)
	}
	res, err := a.serial.call(ctx, "adc_read", channel)
	if err != nil {
		return board.AnalogValue{}, err
	}
	value, ok := toInt(res)
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

// Write is not supported — A0–A5 are input-only analog pins.
func (a *analogPin) Write(_ context.Context, _ int, _ map[string]interface{}) error {
	return fmt.Errorf("analog write not supported on Arduino UNO Q (pins A0-A5 are input only)")
}
