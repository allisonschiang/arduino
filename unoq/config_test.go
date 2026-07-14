package unoq

import (
	"testing"

	"go.viam.com/test"
)

func TestConfigValidateInterrupts(t *testing.T) {
	cfg := &Config{
		DigitalInterrupts: []InterruptConfig{{Name: "enc-a", Pin: "2", Mode: "CHANGE"}},
	}
	_, _, err := cfg.Validate("test")
	test.That(t, err, test.ShouldBeNil)
}

func TestConfigValidateInterruptMissingPin(t *testing.T) {
	cfg := &Config{DigitalInterrupts: []InterruptConfig{{Name: "enc", Pin: ""}}}
	_, _, err := cfg.Validate("test")
	test.That(t, err, test.ShouldNotBeNil)
}

func TestConfigValidateInterruptMissingName(t *testing.T) {
	cfg := &Config{DigitalInterrupts: []InterruptConfig{{Name: "", Pin: "2"}}}
	_, _, err := cfg.Validate("test")
	test.That(t, err, test.ShouldNotBeNil)
}

func TestConfigValidateInterruptModeDefault(t *testing.T) {
	cfg := &Config{DigitalInterrupts: []InterruptConfig{{Name: "btn", Pin: "3"}}}
	_, _, err := cfg.Validate("test")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, cfg.DigitalInterrupts[0].Mode, test.ShouldEqual, "CHANGE")
}

func TestConfigValidateInterruptInvalidMode(t *testing.T) {
	cfg := &Config{DigitalInterrupts: []InterruptConfig{{Name: "x", Pin: "2", Mode: "BOGUS"}}}
	_, _, err := cfg.Validate("test")
	test.That(t, err, test.ShouldNotBeNil)
}

func TestConfigValidateInterruptDuplicateName(t *testing.T) {
	cfg := &Config{DigitalInterrupts: []InterruptConfig{
		{Name: "dup", Pin: "2"}, {Name: "dup", Pin: "3"},
	}}
	_, _, err := cfg.Validate("test")
	test.That(t, err, test.ShouldNotBeNil)
}

// The router socket has a default, so a fully empty config is valid.
func TestConfigValidateEmpty(t *testing.T) {
	cfg := &Config{}
	deps, optional, err := cfg.Validate("test")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, deps, test.ShouldBeNil)
	test.That(t, optional, test.ShouldBeNil)
}

func TestConfigValidateAnalogs(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cfg := &Config{AnalogReaders: []AnalogConfig{{Name: "a0", Pin: "0"}, {Name: "a1", Pin: "1"}}}
		_, _, err := cfg.Validate("test")
		test.That(t, err, test.ShouldBeNil)
	})
	t.Run("missing name", func(t *testing.T) {
		cfg := &Config{AnalogReaders: []AnalogConfig{{Name: "", Pin: "0"}}}
		_, _, err := cfg.Validate("test")
		test.That(t, err, test.ShouldNotBeNil)
	})
	t.Run("duplicate name", func(t *testing.T) {
		cfg := &Config{AnalogReaders: []AnalogConfig{{Name: "a", Pin: "0"}, {Name: "a", Pin: "1"}}}
		_, _, err := cfg.Validate("test")
		test.That(t, err, test.ShouldNotBeNil)
	})
}
