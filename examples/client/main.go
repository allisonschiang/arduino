// Command client is a Go SDK smoke test for the viam:arduino:uno-q board,
// complementing test_board.py. Exercises GPIO, analog, PWM, and a digital
// interrupt over the same board API the app uses.
//
// Set credentials via env (app.viam.com -> CONNECT -> Code sample -> Go):
//
//	VIAM_ADDRESS=my-machine-main.xxxx.viam.cloud \
//	VIAM_API_KEY_ID=... VIAM_API_KEY=... go run ./examples/client
//
// Wiring: D9->A0 (analog + PWM duty), D5->D2 (PWM freq + int2), D13->D3 (digital).
package main

import (
	"context"
	"os"
	"time"

	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/robot/client"
	"go.viam.com/utils/rpc"
)

func main() {
	logger := logging.NewLogger("client")
	ctx := context.Background()

	machine, err := client.New(ctx, os.Getenv("VIAM_ADDRESS"), logger,
		client.WithDialOptions(rpc.WithEntityCredentials(
			os.Getenv("VIAM_API_KEY_ID"),
			rpc.Credentials{Type: rpc.CredentialsTypeAPIKey, Payload: os.Getenv("VIAM_API_KEY")},
		)),
	)
	if err != nil {
		logger.Fatal(err)
	}
	defer machine.Close(ctx)

	b, err := board.FromRobot(machine, "board")
	if err != nil {
		logger.Fatal(err)
	}

	p9, err := b.GPIOPinByName("9")
	if err != nil {
		logger.Fatal(err)
	}
	p5, err := b.GPIOPinByName("5")
	if err != nil {
		logger.Fatal(err)
	}
	p13, err := b.GPIOPinByName("13")
	if err != nil {
		logger.Fatal(err)
	}
	d3, err := b.GPIOPinByName("3")
	if err != nil {
		logger.Fatal(err)
	}
	a0, err := b.AnalogByName("a0")
	if err != nil {
		logger.Fatal(err)
	}
	int2, err := b.DigitalInterruptByName("int2")
	if err != nil {
		logger.Fatal(err)
	}

	// Digital + analog (D9 -> A0): drive D9, read it back on A0.
	_ = p9.Set(ctx, true, nil)
	time.Sleep(50 * time.Millisecond)
	hi, _ := a0.Read(ctx, nil)
	_ = p9.Set(ctx, false, nil)
	time.Sleep(50 * time.Millisecond)
	lo, _ := a0.Read(ctx, nil)
	logger.Infof("analog (D9->A0): high -> a0=%d (expect ~4095), low -> a0=%d (expect ~0)", hi.Value, lo.Value)

	// PWM duty (D9 -> A0): average many ADC samples; mean/4095 ~= duty.
	for _, duty := range []float64{0.25, 0.5, 0.75} {
		_ = p9.SetPWM(ctx, duty, nil)
		time.Sleep(100 * time.Millisecond)
		var sum, cnt int
		for i := 0; i < 100; i++ {
			if v, err := a0.Read(ctx, nil); err == nil {
				sum += v.Value
				cnt++
			}
		}
		logger.Infof("pwm duty (D9->A0): set %.2f -> measured %.2f", duty, float64(sum)/float64(cnt)/4095.0)
	}
	_ = p9.SetPWM(ctx, 0, nil)

	// PWM frequency + interrupts (D5 -> D2/int2): count edges over 1s, /2 = Hz.
	_ = p5.SetPWM(ctx, 0.5, nil)
	for _, hz := range []uint{50, 100, 200} {
		_ = p5.SetPWMFreq(ctx, hz, nil)
		time.Sleep(150 * time.Millisecond)
		before, _ := int2.Value(ctx, nil)
		time.Sleep(time.Second)
		after, _ := int2.Value(ctx, nil)
		logger.Infof("pwm freq + interrupt (D5->D2): set %dHz -> %d edges/s -> measured %.1fHz", hz, after-before, float64(after-before)/2.0)
	}
	_ = p5.SetPWM(ctx, 0, nil)

	// Digital loopback (D13 -> D3): drive low first to establish a baseline, then
	// high, then low again. D3 tracking each change proves the reading follows
	// p13.Set (not a floating/stuck pin).
	for _, want := range []bool{false, true, false} {
		_ = p13.Set(ctx, want, nil)
		time.Sleep(30 * time.Millisecond)
		got, _ := d3.Get(ctx, nil)
		match := "OK"
		if got != want {
			match = "MISMATCH"
		}
		logger.Infof("digital (D13->D3) %s: set %v -> got %v", match, want, got)
	}
}
