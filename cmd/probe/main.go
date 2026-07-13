// Command probe is a router-level smoke test: it dials the arduino-router Unix
// socket directly (bypassing the Viam module) and exercises the firmware RPCs.
// Useful for confirming the sketch is flashed and responding before bringing up
// the module. For full hardware verification against a jumper rig, use cmd/cli.
//
//	probe [socket]
package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

func main() {
	sock := "/var/run/arduino-router.sock"
	if len(os.Args) > 1 {
		sock = os.Args[1]
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		fmt.Println("DIAL FAIL:", err)
		os.Exit(1)
	}
	defer conn.Close()

	enc := msgpack.NewEncoder(conn)
	dec := msgpack.NewDecoder(conn)
	var id uint32
	call := func(method string, args ...interface{}) interface{} {
		id++
		if args == nil {
			args = []interface{}{}
		}
		_ = enc.Encode([]interface{}{0, id, method, args})
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		var resp []interface{}
		if err := dec.Decode(&resp); err != nil {
			return fmt.Sprintf("ERR:%v", err)
		}
		if len(resp) >= 4 && resp[2] != nil {
			return fmt.Sprintf("ERR:%v", resp[2])
		}
		if len(resp) >= 4 {
			return resp[3]
		}
		return nil
	}

	fmt.Println("hello:      ", call("hello"))
	fmt.Println("gpio_set 13:", call("gpio_set", 13, true))
	fmt.Println("gpio_get 13:", call("gpio_get", 13))
	fmt.Println("adc_read 0: ", call("adc_read", 0))
	fmt.Println("pwm_set 9:  ", call("pwm_set", 9, 0.5))
	fmt.Println("pwm_freq 9: ", call("pwm_freq", 9, 1000))
	call("pwm_set", 9, 0.0)
}
