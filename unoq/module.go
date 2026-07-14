// Package unoq implements the Arduino UNO Q board.
package unoq

import (
	"arduino/utils"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "go.viam.com/api/component/board/v1"
	board "go.viam.com/rdk/components/board"
	viamgrpc "go.viam.com/rdk/grpc"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// UnoQ is the board model this module registers.
var UnoQ = resource.NewModel("viam", "arduino", "uno-q")

// firmwareVersion is the value the firmware's "hello" RPC must return.
const firmwareVersion = "UNO-Q v2"

func init() {
	resource.RegisterComponent(board.API, UnoQ,
		resource.Registration[board.Board, *Config]{
			Constructor: newArduinoUnoQ,
		},
	)
}

// AnalogConfig configures a single analog input channel.
type AnalogConfig struct {
	Name string `json:"name"`
	Pin  string `json:"pin"` // "0" through "5" for A0–A5
}

// InterruptConfig configures a single digital interrupt.
type InterruptConfig struct {
	Name string `json:"name"`
	Pin  string `json:"pin"`
	Mode string `json:"mode,omitempty"` // "RISING", "FALLING", or "CHANGE" (default)
}

// Config holds the configuration for the Arduino UNO Q board component.
// RouterSocket is optional and defaults to the standard arduino-router Unix socket.
type Config struct {
	RouterSocket      string            `json:"router_socket,omitempty"`
	AnalogReaders     []AnalogConfig    `json:"analogs,omitempty"`
	DigitalInterrupts []InterruptConfig `json:"digital_interrupts,omitempty"`
}

// Validate ensures the config is valid and fills in defaults. It declares no
// resource dependencies. Returns (requiredDeps, optionalDeps, error).
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	seen := map[string]bool{}
	for i, ar := range cfg.AnalogReaders {
		if ar.Name == "" {
			return nil, nil, resource.NewConfigValidationFieldRequiredError(fmt.Sprintf("%s.analogs.%d", path, i), "name")
		}
		if ar.Pin == "" {
			return nil, nil, resource.NewConfigValidationFieldRequiredError(fmt.Sprintf("%s.analogs.%d", path, i), "pin")
		}
		if seen[ar.Name] {
			return nil, nil, resource.NewConfigValidationError(path, fmt.Errorf("duplicate analog name %q", ar.Name))
		}
		seen[ar.Name] = true
	}
	iseen := map[string]bool{}
	for i := range cfg.DigitalInterrupts {
		ic := &cfg.DigitalInterrupts[i]
		field := fmt.Sprintf("%s.digital_interrupts.%d", path, i)
		if ic.Name == "" {
			return nil, nil, resource.NewConfigValidationFieldRequiredError(field, "name")
		}
		if ic.Pin == "" {
			return nil, nil, resource.NewConfigValidationFieldRequiredError(field, "pin")
		}
		if iseen[ic.Name] {
			return nil, nil, resource.NewConfigValidationError(path, fmt.Errorf("duplicate digital_interrupt name %q", ic.Name))
		}
		iseen[ic.Name] = true
		switch ic.Mode {
		case "":
			ic.Mode = "CHANGE"
		case "RISING", "FALLING", "CHANGE":
		default:
			return nil, nil, resource.NewConfigValidationError(field,
				errors.New(`mode must be one of "RISING", "FALLING", or "CHANGE"`))
		}
	}
	return nil, nil, nil
}

type arduinoUnoQ struct {
	resource.Named
	// AlwaysRebuild: any config change tears the board down and reconstructs it.
	// The transport (router socket) is reopened from scratch anyway, so a clean
	// rebuild is simpler and safer than an in-place reconfigure.
	resource.AlwaysRebuild

	mu         sync.Mutex
	serial     sender
	gpios      map[string]*gpioPin
	analogs    map[string]*analogPin
	interrupts map[string]*digitalInterrupt // keyed by logical name

	tickSubsMu sync.Mutex
	tickSubs   []chan board.Tick // active StreamTicks subscribers

	logger logging.Logger
	cfg    *Config

	cancelFunc func()
}

// NewUnoQ is exported for use by the CLI and testing utilities.
func NewUnoQ(ctx context.Context, _ resource.Dependencies, name resource.Name, conf *Config, logger logging.Logger) (board.Board, error) {
	conn, err := openRPC(conf.RouterSocket)
	if err != nil {
		return nil, err
	}
	return newBoardWithSender(ctx, name, conf, conn, logger)
}

func newArduinoUnoQ(ctx context.Context, _ resource.Dependencies, rawConf resource.Config, logger logging.Logger) (board.Board, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	conn, err := openRPC(conf.RouterSocket)
	if err != nil {
		return nil, err
	}
	return newBoardWithSender(ctx, rawConf.ResourceName(), conf, conn, logger)
}

func newBoardWithSender(ctx context.Context, name resource.Name, conf *Config, s sender, logger logging.Logger) (*arduinoUnoQ, error) {
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	b := &arduinoUnoQ{
		Named:      name.AsNamed(),
		serial:     s,
		gpios:      map[string]*gpioPin{},
		analogs:    map[string]*analogPin{},
		interrupts: map[string]*digitalInterrupt{},
		logger:     logger,
		cfg:        conf,
		cancelFunc: cancelFunc,
	}
	fail := func(err error) (*arduinoUnoQ, error) {
		s.close()
		cancelFunc()
		return nil, err
	}
	if err := b.hello(ctx); err != nil {
		return fail(err)
	}
	// Claim "tick" notifications so the firmware's interrupt edges route to us:
	// arduino-router forwards a notification only to the client that registered
	// its method name — it does not broadcast.
	if err := b.registerTick(ctx); err != nil {
		return fail(err)
	}
	for _, ar := range conf.AnalogReaders {
		b.analogs[ar.Name] = &analogPin{channel: ar.Pin, serial: b.serial}
	}
	if err := b.configureInterrupts(conf.DigitalInterrupts); err != nil {
		return fail(err)
	}
	go b.tickDispatcher(s, cancelCtx)
	return b, nil
}

// registerTick claims the "tick" notification method with arduino-router so the
// firmware's tick notifications are routed to this connection. Tolerates an
// already-registered route.
func (b *arduinoUnoQ) registerTick(ctx context.Context) error {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := b.serial.call(rctx, "$/register", "tick"); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("registering tick notifications: %w", err)
	}
	return nil
}

// hello performs the firmware handshake, retrying until the overall deadline so a
// freshly-booted STM32 (or a router still bringing the sketch up) can catch up.
func (b *arduinoUnoQ) hello(ctx context.Context) error {
	const (
		overallTimeout = 30 * time.Second
		attemptTimeout = 5 * time.Second
		retryWait      = 500 * time.Millisecond
	)
	ctx, cancel := context.WithTimeout(ctx, overallTimeout)
	defer cancel()

	for {
		attemptCtx, attemptCancel := context.WithTimeout(ctx, attemptTimeout)
		res, err := b.serial.call(attemptCtx, "hello")
		attemptCancel()

		if err == nil {
			got, _ := utils.ToString(res)
			if got != firmwareVersion {
				return fmt.Errorf("firmware version mismatch: got %q, want %q", got, firmwareVersion)
			}
			return nil
		}

		if ctx.Err() != nil {
			return fmt.Errorf("hello handshake timed out after %v: %w", overallTimeout, err)
		}
		select {
		case <-time.After(retryWait):
		case <-ctx.Done():
			return fmt.Errorf("hello handshake timed out after %v: %w", overallTimeout, err)
		}
	}
}

// AnalogByName returns a named analog reader from the config.
func (b *arduinoUnoQ) AnalogByName(name string) (board.Analog, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a, ok := b.analogs[name]
	if !ok {
		return nil, fmt.Errorf("analog reader %q not found", name)
	}
	return a, nil
}

// configureInterrupts calls int_config for each configured interrupt and stores a
// digitalInterrupt keyed by logical name.
func (b *arduinoUnoQ) configureInterrupts(cfgs []InterruptConfig) error {
	for _, ic := range cfgs {
		pin, err := utils.PinToInt(ic.Pin)
		if err != nil {
			return fmt.Errorf("interrupt %q: %w", ic.Name, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		res, err := b.serial.call(ctx, "int_config", pin, ic.Mode)
		cancel()
		if err != nil {
			return fmt.Errorf("configuring interrupt %q on pin %s: %w", ic.Name, ic.Pin, err)
		}
		if ok, _ := utils.ToBool(res); !ok {
			return fmt.Errorf("configuring interrupt %q on pin %s: firmware rejected mode %q", ic.Name, ic.Pin, ic.Mode)
		}
		b.interrupts[ic.Name] = &digitalInterrupt{name: ic.Name, pin: ic.Pin}
	}
	return nil
}

// tickDispatcher reads tick notifications from the transport and fans them out to
// all active StreamTicks subscribers.
func (b *arduinoUnoQ) tickDispatcher(s sender, ctx context.Context) {
	ticks := s.ticks()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ticks:
			if !ok {
				return
			}
			b.dispatchTick(ev)
		}
	}
}

// dispatchTick increments the matching interrupt counter and fans the event to
// all StreamTicks callers.
func (b *arduinoUnoQ) dispatchTick(ev tickEvent) {
	pinStr := strconv.Itoa(ev.pin)

	b.mu.Lock()
	var matched *digitalInterrupt
	for _, di := range b.interrupts {
		if di.pin == pinStr {
			di.recordTick()
			matched = di
			break
		}
	}
	b.mu.Unlock()

	if matched == nil {
		return
	}

	tick := board.Tick{
		Name:             matched.name,
		High:             ev.high,
		TimestampNanosec: ev.micros * 1000,
	}

	b.tickSubsMu.Lock()
	subs := make([]chan board.Tick, len(b.tickSubs))
	copy(subs, b.tickSubs)
	b.tickSubsMu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- tick:
		default: // never block a slow subscriber
		}
	}
}

// DigitalInterruptByName looks up a configured interrupt by logical name.
func (b *arduinoUnoQ) DigitalInterruptByName(name string) (board.DigitalInterrupt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	di, ok := b.interrupts[name]
	if !ok {
		return nil, fmt.Errorf("digital interrupt %q not configured", name)
	}
	return di, nil
}

// GPIOPinByName returns (and lazily creates) a GPIO pin by its Arduino pin number.
func (b *arduinoUnoQ) GPIOPinByName(name string) (board.GPIOPin, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.gpios[name]; ok {
		return p, nil
	}
	p := &gpioPin{pinNum: name, serial: b.serial}
	b.gpios[name] = p
	return p, nil
}

// SetPowerMode is not supported by this board.
func (b *arduinoUnoQ) SetPowerMode(_ context.Context, _ pb.PowerMode, _ *time.Duration, _ map[string]interface{}) error {
	return viamgrpc.UnimplementedError
}

func (b *arduinoUnoQ) DoCommand(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	return nil, viamgrpc.UnimplementedError
}

// Status satisfies the board.Board (resource.Resource) interface.
func (b *arduinoUnoQ) Status(_ context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// StreamTicks subscribes ch to all tick events, cleaning up when ctx is cancelled.
func (b *arduinoUnoQ) StreamTicks(ctx context.Context, _ []board.DigitalInterrupt, ch chan board.Tick, _ map[string]interface{}) error {
	b.tickSubsMu.Lock()
	b.tickSubs = append(b.tickSubs, ch)
	b.tickSubsMu.Unlock()

	go func() {
		<-ctx.Done()
		b.tickSubsMu.Lock()
		for i, sub := range b.tickSubs {
			if sub == ch {
				b.tickSubs = append(b.tickSubs[:i], b.tickSubs[i+1:]...)
				break
			}
		}
		b.tickSubsMu.Unlock()
	}()

	return nil
}

func (b *arduinoUnoQ) Close(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelFunc()
	return b.serial.close()
}
