//go:build hardware

// Package unoq_test exercises the board through its exported API against real
// hardware: arduino-router + firmware + the jumper rig. It is gated behind the
// `hardware` build tag so `go test ./...` stays green without a board. Run it on
// an UNO Q (Linux side) with:
//
//	go test -tags hardware ./tests/... -v
//
// Wiring: D9->A0 (analog + PWM duty), D5->D2 (PWM freq + int2), D13->D3 (digital).
package unoq_test

import (
	"context"
	"os"
	"testing"
	"time"

	"arduino/unoq"
	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
)

const routerSocket = "/var/run/arduino-router.sock"

func newHardwareBoard(t *testing.T) board.Board {
	t.Helper()
	if _, err := os.Stat(routerSocket); err != nil {
		t.Skipf("arduino-router socket %s not present; not on hardware", routerSocket)
	}
	cfg := &unoq.Config{
		AnalogReaders:     []unoq.AnalogConfig{{Name: "a0", Pin: "0"}},
		DigitalInterrupts: []unoq.InterruptConfig{{Name: "int2", Pin: "2", Mode: "CHANGE"}},
	}
	b, err := unoq.NewUnoQ(context.Background(), resource.Dependencies{}, board.Named("board"), cfg, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldBeNil)
	t.Cleanup(func() { test.That(t, b.Close(context.Background()), test.ShouldBeNil) })
	return b
}

// Digital loopback: D13 -> D3. Baseline low first so the high reading is provably
// caused by Set, not a floating pin.
func TestHardwareDigital(t *testing.T) {
	ctx := context.Background()
	b := newHardwareBoard(t)
	d13, err := b.GPIOPinByName("13")
	test.That(t, err, test.ShouldBeNil)
	d3, err := b.GPIOPinByName("3")
	test.That(t, err, test.ShouldBeNil)

	for _, want := range []bool{false, true, false} {
		test.That(t, d13.Set(ctx, want, nil), test.ShouldBeNil)
		time.Sleep(30 * time.Millisecond)
		got, err := d3.Get(ctx, nil)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, got, test.ShouldEqual, want)
	}
}

// Analog: D9 -> A0. High reads near full-scale, low near zero.
func TestHardwareAnalog(t *testing.T) {
	ctx := context.Background()
	b := newHardwareBoard(t)
	d9, err := b.GPIOPinByName("9")
	test.That(t, err, test.ShouldBeNil)
	a0, err := b.AnalogByName("a0")
	test.That(t, err, test.ShouldBeNil)

	test.That(t, d9.Set(ctx, true, nil), test.ShouldBeNil)
	time.Sleep(50 * time.Millisecond)
	hi, err := a0.Read(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, hi.Value, test.ShouldBeGreaterThan, 3500)

	test.That(t, d9.Set(ctx, false, nil), test.ShouldBeNil)
	time.Sleep(50 * time.Millisecond)
	lo, err := a0.Read(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, lo.Value, test.ShouldBeLessThan, 300)
}

// PWM duty: D9 -> A0. The ADC average over many samples tracks the set duty.
func TestHardwarePWMDuty(t *testing.T) {
	ctx := context.Background()
	b := newHardwareBoard(t)
	d9, err := b.GPIOPinByName("9")
	test.That(t, err, test.ShouldBeNil)
	a0, err := b.AnalogByName("a0")
	test.That(t, err, test.ShouldBeNil)

	for _, duty := range []float64{0.25, 0.5, 0.75} {
		test.That(t, d9.SetPWM(ctx, duty, nil), test.ShouldBeNil)
		time.Sleep(100 * time.Millisecond)
		var sum, n int
		for i := 0; i < 100; i++ {
			if v, err := a0.Read(ctx, nil); err == nil {
				sum += v.Value
				n++
			}
		}
		measured := float64(sum) / float64(n) / 4095.0
		test.That(t, measured, test.ShouldAlmostEqual, duty, 0.1)
	}
	test.That(t, d9.SetPWM(ctx, 0, nil), test.ShouldBeNil)
}

// PWM frequency + interrupts: D5 -> D2/int2. Setting a frequency makes the int2
// edge counter climb; the rate scales with the setting.
func TestHardwarePWMFreqAndInterrupt(t *testing.T) {
	ctx := context.Background()
	b := newHardwareBoard(t)
	d5, err := b.GPIOPinByName("5")
	test.That(t, err, test.ShouldBeNil)
	int2, err := b.DigitalInterruptByName("int2")
	test.That(t, err, test.ShouldBeNil)

	test.That(t, d5.SetPWM(ctx, 0.5, nil), test.ShouldBeNil)
	test.That(t, d5.SetPWMFreq(ctx, 100, nil), test.ShouldBeNil)
	time.Sleep(150 * time.Millisecond)

	before, err := int2.Value(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	time.Sleep(time.Second)
	after, err := int2.Value(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, after-before, test.ShouldBeGreaterThan, int64(0))
	test.That(t, d5.SetPWM(ctx, 0, nil), test.ShouldBeNil)
}
