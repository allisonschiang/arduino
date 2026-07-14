// Package utils holds board-agnostic helpers for the Arduino UNO Q module:
// Arduino pin parsing and MessagePack value coercion.
package utils

import (
	"fmt"
	"strconv"
)

// PinToInt parses an Arduino pin-number string (e.g. "13") into an int.
func PinToInt(pin string) (int, error) {
	n, err := strconv.Atoi(pin)
	if err != nil {
		return 0, fmt.Errorf("invalid pin %q: not a number", pin)
	}
	return n, nil
}
