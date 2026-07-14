// Command dac is a full-stack test of Analog.Write (the STM32 DAC) over the Viam
// SDK -> viam-server -> module -> hardware. It writes DAC values to A0 and reads
// them back on A2 to confirm the analog output tracks.
//
// Wiring: A0 -> A2 (DAC output on A0, measured on A2's ADC).
// Config: board `analogs` must include both {name:a0, pin:0} and {name:a2, pin:2}.
//
//	VIAM_ADDRESS=... VIAM_API_KEY_ID=... VIAM_API_KEY=... go run ./examples/dac
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
	logger := logging.NewLogger("dac")
	ctx := context.Background()

	machine, err := client.New(ctx, os.Getenv("VIAM_ADDRESS"), logger,
		client.WithDialOptions(rpc.WithEntityCredentials(
			os.Getenv("VIAM_API_KEY_ID"),
			rpc.Credentials{Type: rpc.CredentialsTypeAPIKey, Payload: os.Getenv("VIAM_API_KEY")},
		)))
	if err != nil {
		logger.Fatal(err)
	}
	defer machine.Close(ctx)

	b, err := board.FromRobot(machine, "board")
	if err != nil {
		logger.Fatal(err)
	}
	a0, err := b.AnalogByName("a0") // DAC output (channel 0 = A0)
	if err != nil {
		logger.Fatal(err)
	}
	a2, err := b.AnalogByName("a2") // ADC readback (channel 2 = A2), jumpered from A0
	if err != nil {
		logger.Fatal(err)
	}

	for _, dac := range []int{0, 1024, 2048, 3072, 4095} {
		if err := a0.Write(ctx, dac, nil); err != nil {
			logger.Fatalf("a0.Write(%d): %v", dac, err)
		}
		time.Sleep(80 * time.Millisecond)
		got, err := a2.Read(ctx, nil)
		if err != nil {
			logger.Fatalf("a2.Read: %v", err)
		}
		logger.Infof("DAC (A0->A2): wrote %d (%.2fV) -> read %d (%.2fV)",
			dac, float64(dac)/4095.0*3.3, got.Value, float64(got.Value)/4095.0*3.3)
	}
	_ = a0.Write(ctx, 0, nil)
}
