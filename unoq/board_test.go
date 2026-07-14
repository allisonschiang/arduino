package unoq

import (
	"context"
	"fmt"
	"testing"
	"time"

	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/test"
)

func testBoard(t *testing.T, mock *mockSender) *arduinoUnoQ {
	t.Helper()
	b, err := newBoardWithSender(context.Background(), board.Named("test-board"), &Config{}, mock, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldBeNil)
	return b
}

// newTestBoard builds a board backed by a fresh default mock and returns both.
func newTestBoard(t *testing.T) (*arduinoUnoQ, *mockSender) {
	t.Helper()
	mock := newMockSender()
	return testBoard(t, mock), mock
}

func TestNewBoardHelloHandshake(t *testing.T) {
	t.Run("correct firmware version succeeds", func(t *testing.T) {
		mock := newMockSender()
		b := testBoard(t, mock)
		test.That(t, b, test.ShouldNotBeNil)
		_, called := mock.lastCall("hello")
		test.That(t, called, test.ShouldBeTrue)
	})

	t.Run("wrong firmware version returns error", func(t *testing.T) {
		mock := newMockSender()
		mock.on("hello", func([]interface{}) (interface{}, error) { return "UNO-Q v0", nil })
		_, err := newBoardWithSender(context.Background(), board.Named("test-board"), &Config{}, mock, logging.NewTestLogger(t))
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "version mismatch")
	})
}

func TestNewBoardHelloErr(t *testing.T) {
	mock := newMockSender()
	mock.on("hello", func([]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("router connection closed")
	})
	// Short context so hello()'s retry loop exits quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := newBoardWithSender(ctx, board.Named("test-board"), &Config{}, mock, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldNotBeNil)
	// close() must have been called to clean up after failure.
	test.That(t, mock.isClosed(), test.ShouldBeTrue)
}

func TestBoardClose(t *testing.T) {
	mock := newMockSender()
	b := testBoard(t, mock)
	test.That(t, b.Close(context.Background()), test.ShouldBeNil)
	test.That(t, mock.isClosed(), test.ShouldBeTrue)
}

func TestBoardAnalogFromConfig(t *testing.T) {
	mock := newMockSender()
	conf := &Config{
		AnalogReaders: []AnalogConfig{
			{Name: "a0", Pin: "0"},
			{Name: "a1", Pin: "1"},
		},
	}
	b, err := newBoardWithSender(context.Background(), board.Named("test-board"), conf, mock, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldBeNil)

	a, err := b.AnalogByName("a0")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, a, test.ShouldNotBeNil)

	_, err = b.AnalogByName("a1")
	test.That(t, err, test.ShouldBeNil)

	_, err = b.AnalogByName("a2")
	test.That(t, err, test.ShouldNotBeNil)
}
