// Command cli is an on-chip hardware harness for the viam:arduino:uno-q board.
// It drives the real module code (NewUnoQ -> arduino-router) directly, with no
// viam-server or cloud connection, and exercises all five goals against a fixed
// jumper rig:
//
//	D9  -> A0   analog read + PWM duty
//	D5  -> D2   PWM frequency (edge-counted on int2) + interrupts
//	D13 -> D3   digital set/get loopback
//
// Run on the UNO Q Linux side: ./cli            (all tests)
//
//	./cli -pin 13    (legacy single-pin read)
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"arduino/unoq"
	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

func main() {
	socket := flag.String("socket", "", "arduino-router socket path (default /var/run/arduino-router.sock)")
	pin := flag.String("pin", "", "if set, just read this GPIO pin and exit")
	flag.Parse()

	ctx := context.Background()
	logger := logging.NewLogger("cli")

	cfg := unoq.Config{
		RouterSocket:  *socket,
		AnalogReaders: []unoq.AnalogConfig{{Name: "a0", Pin: "0"}},
		DigitalInterrupts: []unoq.InterruptConfig{
			{Name: "int2", Pin: "2", Mode: "CHANGE"},
		},
	}

	b, err := unoq.NewUnoQ(ctx, resource.Dependencies{}, board.Named("board"), &cfg, logger)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer b.Close(ctx)
	log.Print("connected to Arduino UNO Q via arduino-router")

	if *pin != "" {
		p, _ := b.GPIOPinByName(*pin)
		v, err := p.Get(ctx, nil)
		if err != nil {
			log.Fatalf("get pin %s: %v", *pin, err)
		}
		log.Printf("pin %s = %v", *pin, v)
		return
	}

	testDigital(ctx, b)
	testAnalog(ctx, b)
	testPWMDuty(ctx, b)
	testPWMFreq(ctx, b)
}

// testDigital: D13 -> D3 loopback. Set D13, read it back on D3.
func testDigital(ctx context.Context, b board.Board) {
	d13, _ := b.GPIOPinByName("13")
	d3, _ := b.GPIOPinByName("3")
	for _, want := range []bool{true, false, true, false} {
		if err := d13.Set(ctx, want, nil); err != nil {
			log.Printf("DIGITAL: set D13: %v", err)
			return
		}
		time.Sleep(30 * time.Millisecond)
		got, err := d3.Get(ctx, nil)
		if err != nil {
			log.Printf("DIGITAL: get D3: %v", err)
			return
		}
		status := "OK"
		if got != want {
			status = "MISMATCH"
		}
		log.Printf("DIGITAL %s: D13=%v -> D3=%v", status, want, got)
	}
}

// testAnalog: D9 -> A0. Drive D9 high/low, expect a0 near full-scale / zero.
func testAnalog(ctx context.Context, b board.Board) {
	d9, _ := b.GPIOPinByName("9")
	a0, _ := b.AnalogByName("a0")
	_ = d9.Set(ctx, true, nil)
	time.Sleep(50 * time.Millisecond)
	hi, _ := a0.Read(ctx, nil)
	_ = d9.Set(ctx, false, nil)
	time.Sleep(50 * time.Millisecond)
	lo, _ := a0.Read(ctx, nil)
	log.Printf("ANALOG: D9 high -> a0=%d (expect ~4095), low -> a0=%d (expect ~0)", hi.Value, lo.Value)
}

// testPWMDuty: D9 -> A0. Set duty, average many ADC samples; mean/4095 ~= duty.
func testPWMDuty(ctx context.Context, b board.Board) {
	d9, _ := b.GPIOPinByName("9")
	a0, _ := b.AnalogByName("a0")
	for _, duty := range []float64{0.25, 0.5, 0.75} {
		if err := d9.SetPWM(ctx, duty, nil); err != nil {
			log.Printf("PWM DUTY: SetPWM: %v", err)
			return
		}
		time.Sleep(100 * time.Millisecond)
		var sum, n int
		for i := 0; i < 200; i++ {
			v, err := a0.Read(ctx, nil)
			if err == nil {
				sum += v.Value
				n++
			}
		}
		mean := 0.0
		if n > 0 {
			mean = float64(sum) / float64(n)
		}
		log.Printf("PWM DUTY: set %.2f -> mean a0=%.0f, measured duty=%.2f", duty, mean, mean/4095.0)
	}
	_ = d9.SetPWM(ctx, 0, nil)
}

// testPWMFreq: D5 -> D2. Set frequency on D5, count int2 edges over a window.
// edges/sec / 2 ~= Hz (CHANGE fires on both rising and falling).
func testPWMFreq(ctx context.Context, b board.Board) {
	d5, _ := b.GPIOPinByName("5")
	int2, err := b.DigitalInterruptByName("int2")
	if err != nil {
		log.Printf("PWM FREQ: int2 lookup: %v", err)
		return
	}
	if err := d5.SetPWM(ctx, 0.5, nil); err != nil {
		log.Printf("PWM FREQ: SetPWM D5: %v", err)
		return
	}
	const window = time.Second
	for _, hz := range []uint{50, 100, 200} {
		if err := d5.SetPWMFreq(ctx, hz, nil); err != nil {
			log.Printf("PWM FREQ: SetPWMFreq(%d): %v", hz, err)
			continue
		}
		time.Sleep(150 * time.Millisecond) // settle
		before, _ := int2.Value(ctx, nil)
		time.Sleep(window)
		after, _ := int2.Value(ctx, nil)
		edges := after - before
		measured := float64(edges) / window.Seconds() / 2.0
		log.Printf("PWM FREQ: set %dHz -> %d edges/%v -> measured %.1fHz", hz, edges, window, measured)
	}
	_ = d5.SetPWM(ctx, 0, nil)
}
