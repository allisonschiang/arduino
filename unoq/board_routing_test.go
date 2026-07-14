package unoq

import (
	"sync"
	"testing"

	board "go.viam.com/rdk/components/board"
	"go.viam.com/test"
)

func TestGPIOPinByName(t *testing.T) {
	b, _ := newTestBoard(t)

	pin, err := b.GPIOPinByName("13")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, pin, test.ShouldNotBeNil)

	// Same name returns the same cached object.
	pin2, err := b.GPIOPinByName("13")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, pin2, test.ShouldEqual, pin)
}

func TestAnalogByName(t *testing.T) {
	b, _ := newTestBoard(t)
	b.analogs["adc0"] = &analogPin{channel: "0", serial: b.serial}

	a, err := b.AnalogByName("adc0")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, a, test.ShouldNotBeNil)
}

func TestAnalogByNameNotFound(t *testing.T) {
	b, _ := newTestBoard(t)
	_, err := b.AnalogByName("missing")
	test.That(t, err, test.ShouldNotBeNil)
}

func TestGPIOPinByNameConcurrent(t *testing.T) {
	b, _ := newTestBoard(t)

	const goroutines = 20
	pins := make([]board.GPIOPin, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			p, err := b.GPIOPinByName("7")
			test.That(t, err, test.ShouldBeNil)
			pins[idx] = p
		}()
	}
	wg.Wait()

	// All goroutines must have received the identical cached pin object.
	for i := 1; i < goroutines; i++ {
		test.That(t, pins[i], test.ShouldEqual, pins[0])
	}
}
