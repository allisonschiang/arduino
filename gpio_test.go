package arduino

import (
	"context"
	"fmt"
	"testing"

	"go.viam.com/test"
)

func TestGPIOPinGet(t *testing.T) {
	mock := newMockSender()
	mock.on("gpio_get", func([]interface{}) (interface{}, error) { return true, nil })
	p := &gpioPin{pinNum: "13", serial: mock}

	high, err := p.Get(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, high, test.ShouldBeTrue)

	c, ok := mock.lastCall("gpio_get")
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, c.args, test.ShouldResemble, []interface{}{13})
}

func TestGPIOPinSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		high bool
	}{{"high", true}, {"low", false}} {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMockSender()
			p := &gpioPin{pinNum: "13", serial: mock}
			test.That(t, p.Set(context.Background(), tc.high, nil), test.ShouldBeNil)
			c, ok := mock.lastCall("gpio_set")
			test.That(t, ok, test.ShouldBeTrue)
			test.That(t, c.args, test.ShouldResemble, []interface{}{13, tc.high})
		})
	}
}

func TestGPIOPinGetError(t *testing.T) {
	mock := newMockSender()
	mock.on("gpio_get", func([]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("firmware error: bad pin")
	})
	p := &gpioPin{pinNum: "13", serial: mock}
	_, err := p.Get(context.Background(), nil)
	test.That(t, err, test.ShouldNotBeNil)
}

func TestGPIOPinSetPWM(t *testing.T) {
	mock := newMockSender()
	p := &gpioPin{pinNum: "9", serial: mock}

	test.That(t, p.SetPWM(context.Background(), 0.5, nil), test.ShouldBeNil)
	c, ok := mock.lastCall("pwm_set")
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, c.args, test.ShouldResemble, []interface{}{9, 0.5})

	// PWM() returns the cached duty (firmware can't read it back).
	duty, err := p.PWM(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, duty, test.ShouldEqual, 0.5)
}

func TestGPIOPinSetPWMNonPWMPin(t *testing.T) {
	mock := newMockSender()
	p := &gpioPin{pinNum: "4", serial: mock} // 4 is not PWM-capable
	err := p.SetPWM(context.Background(), 0.5, nil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "does not support PWM")
}

func TestGPIOPinSetPWMFirmwareReject(t *testing.T) {
	mock := newMockSender()
	mock.on("pwm_set", func([]interface{}) (interface{}, error) { return false, nil })
	p := &gpioPin{pinNum: "9", serial: mock}
	err := p.SetPWM(context.Background(), 0.5, nil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "rejected")
}

func TestGPIOPinPWMFreq(t *testing.T) {
	mock := newMockSender()
	p := &gpioPin{pinNum: "9", serial: mock}

	test.That(t, p.SetPWMFreq(context.Background(), 1000, nil), test.ShouldBeNil)
	c, ok := mock.lastCall("pwm_freq")
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, c.args, test.ShouldResemble, []interface{}{9, 1000})

	freq, err := p.PWMFreq(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, freq, test.ShouldEqual, uint(1000))
}
