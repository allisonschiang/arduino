package arduino

import (
	"context"
	"testing"

	"go.viam.com/test"
)

func TestDigitalInterruptName(t *testing.T) {
	di := &digitalInterrupt{name: "enc-a"}
	test.That(t, di.Name(), test.ShouldEqual, "enc-a")
}

func TestDigitalInterruptValueStartsZero(t *testing.T) {
	di := &digitalInterrupt{name: "btn"}
	v, err := di.Value(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, v, test.ShouldEqual, int64(0))
}

func TestDigitalInterruptValueCountsTicks(t *testing.T) {
	di := &digitalInterrupt{name: "enc-a"}
	di.recordTick()
	di.recordTick()
	v, err := di.Value(context.Background(), nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, v, test.ShouldEqual, int64(2))
}
