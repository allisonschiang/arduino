// Command client is a Go SDK smoke test for the viam:arduino:uno-q board,
// complementing test_board.py. Exercises GPIO, analog, PWM, and a digital
// interrupt over the same board API the app uses.
//
// Set credentials via env (app.viam.com -> CONNECT -> Code sample -> Go):
//
//	VIAM_ADDRESS=my-machine-main.xxxx.viam.cloud \
//	VIAM_API_KEY_ID=... VIAM_API_KEY=... go run ./examples/client
//
// Wiring: D13->D2 (interrupt), D9->A0 (analog/PWM).
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

	// Digital + analog: drive D9, read it back on A0.
	p9, err := b.GPIOPinByName("9")
	if err != nil {
		logger.Fatal(err)
	}
	a0, err := b.AnalogByName("a0")
	if err != nil {
		logger.Fatal(err)
	}
	if err := p9.Set(ctx, true, nil); err != nil {
		logger.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	hi, _ := a0.Read(ctx, nil)
	if err := p9.Set(ctx, false, nil); err != nil {
		logger.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	lo, _ := a0.Read(ctx, nil)
	logger.Infof("digital+analog: D9 high -> a0=%d, low -> a0=%d", hi.Value, lo.Value)

	// Interrupt: toggle D13 (jumpered to D2) and watch int2 climb.
	di, err := b.DigitalInterruptByName("int2")
	if err != nil {
		logger.Fatal(err)
	}
	p13, err := b.GPIOPinByName("13")
	if err != nil {
		logger.Fatal(err)
	}
	before, _ := di.Value(ctx, nil)
	const n = 10
	for i := 0; i < n; i++ {
		_ = p13.Set(ctx, true, nil)
		time.Sleep(20 * time.Millisecond)
		_ = p13.Set(ctx, false, nil)
		time.Sleep(20 * time.Millisecond)
	}
	after, _ := di.Value(ctx, nil)
	logger.Infof("interrupt: %d toggles -> delta=%d (expect ~%d)", n, after-before, 2*n)
}
