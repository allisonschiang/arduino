package unoq

import (
	"arduino/utils"
	"context"
	"net"
	"testing"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"go.viam.com/test"
)

// fakeServer runs a minimal msgpack-rpc peer on one end of a net.Pipe. It reads
// one request and replies via respond(id) -> (result, isError).
func TestRPCClientCall(t *testing.T) {
	cli, srv := net.Pipe()
	c := newRPCClient(cli)
	defer c.close()

	go func() {
		dec := msgpack.NewDecoder(srv)
		enc := msgpack.NewEncoder(srv)
		var req []interface{}
		if err := dec.Decode(&req); err != nil {
			return
		}
		// req = [0, id, method, params]
		id, _ := utils.ToUint32(req[1])
		_ = enc.Encode([]interface{}{msgResponse, id, nil, "UNO-Q v2"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := c.call(ctx, "hello")
	test.That(t, err, test.ShouldBeNil)
	s, _ := utils.ToString(res)
	test.That(t, s, test.ShouldEqual, "UNO-Q v2")
}

func TestRPCClientCallError(t *testing.T) {
	cli, srv := net.Pipe()
	c := newRPCClient(cli)
	defer c.close()

	go func() {
		dec := msgpack.NewDecoder(srv)
		enc := msgpack.NewEncoder(srv)
		var req []interface{}
		if err := dec.Decode(&req); err != nil {
			return
		}
		id, _ := utils.ToUint32(req[1])
		_ = enc.Encode([]interface{}{msgResponse, id, "bad pin", nil})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.call(ctx, "gpio_get", 99)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "bad pin")
}

func TestRPCClientNotificationTick(t *testing.T) {
	cli, srv := net.Pipe()
	c := newRPCClient(cli)
	defer c.close()

	enc := msgpack.NewEncoder(srv)
	go func() {
		_ = enc.Encode([]interface{}{msgNotification, "tick", []interface{}{2, 1, 9000}})
	}()

	select {
	case ev := <-c.ticks():
		test.That(t, ev.pin, test.ShouldEqual, 2)
		test.That(t, ev.high, test.ShouldBeTrue)
		test.That(t, ev.micros, test.ShouldEqual, uint64(9000))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tick notification")
	}
}

func TestRPCClientCallAfterClose(t *testing.T) {
	cli, _ := net.Pipe()
	c := newRPCClient(cli)
	test.That(t, c.close(), test.ShouldBeNil)

	_, err := c.call(context.Background(), "hello")
	test.That(t, err, test.ShouldNotBeNil)
}
