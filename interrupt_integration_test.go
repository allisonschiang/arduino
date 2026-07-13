package arduino

import (
	"context"
	"testing"
	"time"

	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

func TestDigitalInterruptByNameFound(t *testing.T) {
	mock := newMockSender()
	conf := &Config{
		DigitalInterrupts: []InterruptConfig{{Name: "enc-a", Pin: "2", Mode: "CHANGE"}},
	}
	b, err := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldBeNil)

	di, err := b.DigitalInterruptByName("enc-a")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, di.Name(), test.ShouldEqual, "enc-a")

	// int_config should have been called for the configured pin.
	c, ok := mock.lastCall("int_config")
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, c.args, test.ShouldResemble, []interface{}{2, "CHANGE"})
}

// The board must $/register "tick" on connect — arduino-router routes a
// notification only to the client that registered its method name (it does not
// broadcast), so without this the firmware's tick notifications never arrive.
func TestRegistersTickOnConnect(t *testing.T) {
	mock := newMockSender()
	_, err := newBoardWithSender(context.Background(), board.Named("test"), &Config{}, mock, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldBeNil)
	c, ok := mock.lastCall("$/register")
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, c.args, test.ShouldResemble, []interface{}{"tick"})
}

func TestDigitalInterruptByNameNotFound(t *testing.T) {
	b, _ := newTestBoard(t)
	defer b.Close(context.Background())
	_, err := b.DigitalInterruptByName("missing")
	test.That(t, err, test.ShouldNotBeNil)
}

func TestStreamTicksReceivesTick(t *testing.T) {
	mock := newMockSender()
	conf := &Config{
		DigitalInterrupts: []InterruptConfig{{Name: "btn", Pin: "2", Mode: "RISING"}},
	}
	b, err := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldBeNil)

	di, err := b.DigitalInterruptByName("btn")
	test.That(t, err, test.ShouldBeNil)

	ch := make(chan board.Tick, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	test.That(t, b.StreamTicks(ctx, []board.DigitalInterrupt{di}, ch, nil), test.ShouldBeNil)

	// Let StreamTicks register before injecting the tick.
	time.Sleep(20 * time.Millisecond)

	// Simulate an interrupt edge arriving from the firmware.
	mock.pushTick(tickEvent{pin: 2, high: true, micros: 9000})

	select {
	case tick := <-ch:
		test.That(t, tick.Name, test.ShouldEqual, "btn")
		test.That(t, tick.High, test.ShouldBeTrue)
		test.That(t, tick.TimestampNanosec, test.ShouldEqual, uint64(9000*1000))
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tick")
	}

	// The interrupt's cumulative counter should have advanced.
	v, err := di.Value(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, v, test.ShouldEqual, int64(1))
}
