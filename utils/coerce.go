package utils

// MessagePack value coercion. vmihailenco/msgpack decodes untyped arrays into
// []interface{} with numeric elements as int64/uint64/float64 depending on the
// wire type; these normalize a decoded value to the Go type the caller wants.

// ToInt coerces any decoded numeric value to int.
func ToInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// ToUint32 coerces any decoded numeric value to uint32.
func ToUint32(v interface{}) (uint32, bool) {
	if n, ok := ToInt(v); ok {
		return uint32(n), true
	}
	return 0, false
}

// ToUint64 coerces any decoded numeric value to uint64.
func ToUint64(v interface{}) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case int64:
		return uint64(n), true
	default:
		if i, ok := ToInt(v); ok {
			return uint64(i), true
		}
	}
	return 0, false
}

// ToBool coerces a decoded bool or numeric value to bool (nonzero = true).
func ToBool(v interface{}) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	default:
		if i, ok := ToInt(v); ok {
			return i != 0, true
		}
	}
	return false, false
}

// ToString returns the value as a string if it is one.
func ToString(v interface{}) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
