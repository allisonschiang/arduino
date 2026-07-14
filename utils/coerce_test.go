package utils

import (
	"testing"

	"go.viam.com/test"
)

func TestCoercionHelpers(t *testing.T) {
	i, ok := ToInt(int64(42))
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, i, test.ShouldEqual, 42)

	b, ok := ToBool(true)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, b, test.ShouldBeTrue)

	// numeric-as-bool
	b2, ok := ToBool(int64(1))
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, b2, test.ShouldBeTrue)

	u, ok := ToUint64(uint64(9000))
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, u, test.ShouldEqual, uint64(9000))

	s, ok := ToString("hi")
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, s, test.ShouldEqual, "hi")
}

func TestPinToInt(t *testing.T) {
	n, err := PinToInt("13")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, n, test.ShouldEqual, 13)

	_, err = PinToInt("abc")
	test.That(t, err, test.ShouldNotBeNil)
}
