package unoq

import (
	"arduino/utils"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/vmihailenco/msgpack/v5"
)

// defaultRouterSocket is the Unix socket the arduino-router service listens on.
const defaultRouterSocket = "/var/run/arduino-router.sock"

// MessagePack-RPC message type ids (https://github.com/msgpack-rpc/msgpack-rpc/blob/master/spec.md).
const (
	msgRequest      = 0 // [0, msgid, method, params]
	msgResponse     = 1 // [1, msgid, error, result]
	msgNotification = 2 // [2, method, params]
)

// tickEvent is a decoded "tick" notification pushed by the firmware on an
// interrupt edge.
type tickEvent struct {
	pin    int
	high   bool
	micros uint64
}

// sender is the transport the board logic uses to reach the STM32 through the
// arduino-router MessagePack-RPC bridge. The real implementation is *rpcClient;
// unit tests use a mock, so the board logic is transport-agnostic and testable
// without hardware.
type sender interface {
	// call invokes a firmware-registered RPC method and returns its result.
	call(ctx context.Context, method string, args ...interface{}) (interface{}, error)
	// ticks returns the channel of interrupt-edge notifications.
	ticks() <-chan tickEvent
	close() error
}

type rpcResult struct {
	result interface{}
	err    error
}

// rpcClient is a minimal MessagePack-RPC client over a stream connection
// (the arduino-router Unix socket). A single reader goroutine owns the decode
// path and dispatches responses to pending calls and "tick" notifications to
// tickCh.
type rpcClient struct {
	conn net.Conn
	enc  *msgpack.Encoder

	writeMu sync.Mutex // serializes concurrent request writes
	nextID  uint32     // atomic

	pendingMu sync.Mutex
	pending   map[uint32]chan rpcResult

	tickCh    chan tickEvent
	done      chan struct{}
	closeOnce sync.Once
}

// openRPC dials the arduino-router socket and returns a ready client.
func openRPC(socketPath string) (*rpcClient, error) {
	if socketPath == "" {
		socketPath = defaultRouterSocket
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dialing router socket %s: %w", socketPath, err)
	}
	return newRPCClient(conn), nil
}

// newRPCClient wraps an existing connection (used by tests via net.Pipe).
func newRPCClient(conn net.Conn) *rpcClient {
	c := &rpcClient{
		conn:    conn,
		enc:     msgpack.NewEncoder(conn),
		pending: map[uint32]chan rpcResult{},
		tickCh:  make(chan tickEvent, 256),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *rpcClient) allocID() uint32 {
	return atomic.AddUint32(&c.nextID, 1)
}

// readLoop decodes messages until the connection closes.
func (c *rpcClient) readLoop() {
	dec := msgpack.NewDecoder(c.conn)
	for {
		var msg []interface{}
		if err := dec.Decode(&msg); err != nil {
			c.failAllPending(fmt.Errorf("router connection closed: %w", err))
			return
		}
		if len(msg) == 0 {
			continue
		}
		typ, ok := utils.ToInt(msg[0])
		if !ok {
			continue
		}
		switch typ {
		case msgResponse:
			if len(msg) < 4 {
				continue
			}
			id, ok := utils.ToUint32(msg[1])
			if !ok {
				continue
			}
			var res rpcResult
			if msg[2] != nil {
				res.err = fmt.Errorf("firmware error: %v", msg[2])
			} else {
				res.result = msg[3]
			}
			c.deliver(id, res)
		case msgNotification:
			if len(msg) < 3 {
				continue
			}
			method, _ := msg[1].(string)
			// The router routes "tick" notifications only to the client that
			// registered the method name (see the module's registerTick).
			if method == "tick" {
				c.handleTick(msg[2])
			}
		}
	}
}

func (c *rpcClient) deliver(id uint32, res rpcResult) {
	c.pendingMu.Lock()
	ch, ok := c.pending[id]
	c.pendingMu.Unlock()
	if !ok {
		return
	}
	// Non-blocking: the channel is buffered 1 and expects exactly one value.
	// A drop (duplicate/late response, or the caller already gave up on ctx)
	// must never wedge the single reader goroutine.
	select {
	case ch <- res:
	default:
	}
}

func (c *rpcClient) failAllPending(err error) {
	// Collect channels under the lock, then send after releasing it so a full
	// channel can never deadlock while holding pendingMu.
	c.pendingMu.Lock()
	chans := make([]chan rpcResult, 0, len(c.pending))
	for id, ch := range c.pending {
		chans = append(chans, ch)
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- rpcResult{err: err}:
		default:
		}
	}
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *rpcClient) handleTick(params interface{}) {
	arr, ok := params.([]interface{})
	if !ok || len(arr) < 3 {
		return
	}
	pin, _ := utils.ToInt(arr[0])
	high, _ := utils.ToInt(arr[1])
	micros, _ := utils.ToUint64(arr[2])
	ev := tickEvent{pin: pin, high: high != 0, micros: micros}
	select {
	case c.tickCh <- ev:
	case <-c.done:
	default: // never block the reader on a slow consumer
	}
}

func (c *rpcClient) call(ctx context.Context, method string, args ...interface{}) (interface{}, error) {
	select {
	case <-c.done:
		return nil, fmt.Errorf("router client closed")
	default:
	}

	id := c.allocID()
	ch := make(chan rpcResult, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	if args == nil {
		args = []interface{}{}
	}
	req := []interface{}{msgRequest, id, method, args}

	c.writeMu.Lock()
	err := c.enc.Encode(req)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("sending %q: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%q: %w", method, ctx.Err())
	case <-c.done:
		return nil, fmt.Errorf("%q: router client closed", method)
	case res := <-ch:
		return res.result, res.err
	}
}

func (c *rpcClient) ticks() <-chan tickEvent {
	return c.tickCh
}

func (c *rpcClient) close() error {
	c.closeOnce.Do(func() { close(c.done) })
	return c.conn.Close()
}
