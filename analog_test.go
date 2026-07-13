package arduino

import (
	"context"
	"math"
	"testing"

	"go.viam.com/test"
)

func TestAnalogPinRead(t *testing.T) {
	mock := newMockSender()
	mock.on("adc_read", func([]interface{}) (interface{}, error) { return 2048, nil })
	pin := &analogPin{channel: "0", serial: mock}

	val, err := pin.Read(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, val.Value, test.ShouldEqual, 2048)
	test.That(t, math.Abs(float64(val.Max)-3.3) < 1e-4, test.ShouldBeTrue)

	c, ok := mock.lastCall("adc_read")
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, c.args, test.ShouldResemble, []interface{}{0})
}

func TestAnalogPinReadMax(t *testing.T) {
	mock := newMockSender()
	mock.on("adc_read", func([]interface{}) (interface{}, error) { return 4095, nil })
	pin := &analogPin{channel: "3", serial: mock}
	val, err := pin.Read(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, val.Value, test.ShouldEqual, 4095)
}

func TestAnalogPinWriteNotSupported(t *testing.T) {
	pin := &analogPin{channel: "0", serial: newMockSender()}
	test.That(t, pin.Write(context.Background(), 0, nil), test.ShouldNotBeNil)
}

func TestAnalogPinReadFirmwareReject(t *testing.T) {
	mock := newMockSender()
	// -1 signals an invalid channel from the firmware.
	mock.on("adc_read", func([]interface{}) (interface{}, error) { return -1, nil })
	pin := &analogPin{channel: "9", serial: mock}
	_, err := pin.Read(context.Background(), nil)
	test.That(t, err, test.ShouldNotBeNil)
}

func TestAnalogPinReadStepSize(t *testing.T) {
	mock := newMockSender()
	mock.on("adc_read", func([]interface{}) (interface{}, error) { return 1000, nil })
	pin := &analogPin{channel: "0", serial: mock}
	val, err := pin.Read(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	want := float32(3.3 / 4095.0)
	test.That(t, math.Abs(float64(val.StepSize)-float64(want)) < 1e-6, test.ShouldBeTrue)
}
