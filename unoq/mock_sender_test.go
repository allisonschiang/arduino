package unoq

import (
	"context"
	"fmt"
	"sync"
)

type mockCall struct {
	method string
	args   []interface{}
}

type mockHandler func(args []interface{}) (interface{}, error)

// mockSender implements sender for hardware-free tests. It records every RPC call
// and dispatches to per-method handlers. Sensible defaults let a board construct
// cleanly; tests override behavior via on() and inject interrupts via pushTick().
type mockSender struct {
	mu       sync.Mutex
	handlers map[string]mockHandler
	calls    []mockCall
	tickCh   chan tickEvent
	closed   bool
}

func newMockSender() *mockSender {
	m := &mockSender{
		handlers: map[string]mockHandler{},
		tickCh:   make(chan tickEvent, 64),
	}
	m.on("hello", func([]interface{}) (interface{}, error) { return firmwareVersion, nil })
	m.on("$/register", func([]interface{}) (interface{}, error) { return nil, nil })
	m.on("gpio_set", func([]interface{}) (interface{}, error) { return true, nil })
	m.on("gpio_get", func([]interface{}) (interface{}, error) { return false, nil })
	m.on("adc_read", func([]interface{}) (interface{}, error) { return 0, nil })
	m.on("pwm_set", func([]interface{}) (interface{}, error) { return true, nil })
	m.on("pwm_freq", func([]interface{}) (interface{}, error) { return true, nil })
	m.on("int_config", func([]interface{}) (interface{}, error) { return true, nil })
	return m
}

func (m *mockSender) on(method string, h mockHandler) {
	m.mu.Lock()
	m.handlers[method] = h
	m.mu.Unlock()
}

func (m *mockSender) call(_ context.Context, method string, args ...interface{}) (interface{}, error) {
	m.mu.Lock()
	m.calls = append(m.calls, mockCall{method: method, args: args})
	h, ok := m.handlers[method]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("mockSender: no handler for %q", method)
	}
	return h(args)
}

func (m *mockSender) ticks() <-chan tickEvent { return m.tickCh }

func (m *mockSender) close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

func (m *mockSender) pushTick(ev tickEvent) { m.tickCh <- ev }

// lastCall returns the most recent call to method, or (mockCall{}, false).
func (m *mockSender) lastCall(method string) (mockCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.calls) - 1; i >= 0; i-- {
		if m.calls[i].method == method {
			return m.calls[i], true
		}
	}
	return mockCall{}, false
}

func (m *mockSender) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}
