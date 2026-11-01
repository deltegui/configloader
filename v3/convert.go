package configloader

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
	durationType    = reflect.TypeFor[time.Duration]()
)

func checkType(t reflect.Type) error {
	if reflect.PointerTo(t).Implements(textUnmarshaler) || t == durationType {
		return nil
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return nil
	case reflect.Slice:
		return checkType(t.Elem())
	}
	return fmt.Errorf("type %s is not supported", t)
}

// setString parses text into v.
func setString(v reflect.Value, s string) error {
	t := v.Type()
	if v.CanAddr() && reflect.PointerTo(t).Implements(textUnmarshaler) {
		return v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
	}
	if t == durationType {
		d, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil {
			return err
		}
		v.SetInt(int64(d))
		return nil
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err != nil {
			return err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(s), 0, t.Bits())
		if err != nil {
			return err
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(strings.TrimSpace(s), 0, t.Bits())
		if err != nil {
			return err
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(strings.TrimSpace(s), t.Bits())
		if err != nil {
			return err
		}
		v.SetFloat(n)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte(s))
			return nil
		}
		return setList(v, splitList(s))
	default:
		return fmt.Errorf("type %s is not supported", t)
	}
	return nil
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func setList(v reflect.Value, items []string) error {
	out := reflect.MakeSlice(v.Type(), len(items), len(items))
	for i, item := range items {
		if err := setString(out.Index(i), item); err != nil {
			return fmt.Errorf("item %d: %w", i+1, err)
		}
	}
	v.Set(out)
	return nil
}

// setAny stores a value decoded from JSON.
func setAny(v reflect.Value, x any) error {
	if list, ok := x.([]any); ok {
		if v.Kind() != reflect.Slice || v.Type().Elem().Kind() == reflect.Uint8 {
			return errors.New("got a list")
		}
		items := make([]string, len(list))
		for i, e := range list {
			s, err := scalar(e)
			if err != nil {
				return fmt.Errorf("item %d: %w", i+1, err)
			}
			items[i] = s
		}
		return setList(v, items)
	}
	s, err := scalar(x)
	if err != nil {
		return err
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() != reflect.Uint8 {
		return errors.New("expected a list")
	}
	return setString(v, s)
}

func scalar(x any) (string, error) {
	switch x := x.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case bool:
		return strconv.FormatBool(x), nil
	}
	return "", fmt.Errorf("unexpected %T", x)
}
