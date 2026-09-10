// Package jsonx provides an order-preserving JSON value model that mirrors
// serde_json with the preserve_order feature enabled.
//
// Protocol payloads carry arbitrary JSON (tool parameter schemas, metadata).
// serde_json keeps object keys in insertion order and re-serializes numbers
// with Rust's shortest round-trip formatting; this package reproduces that
// behaviour so converted prompts and responses are byte-identical.
package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Value is one JSON value. It is one of:
//
//	nil, bool, string, int64, uint64, float64, Array, *Object
//
// The numeric types mirror serde_json::Number: integers that fit in i64 or u64
// keep their exact value, everything else is a float64.
type Value = any

// Array is a JSON array.
type Array = []Value

// Object is a JSON object that preserves key insertion order.
type Object struct {
	keys   []string
	values map[string]Value
}

// NewObject returns an empty object.
func NewObject() *Object {
	return &Object{values: make(map[string]Value)}
}

// Set inserts or replaces a key, keeping the position of an existing key.
func (o *Object) Set(key string, value Value) {
	if o.values == nil {
		o.values = make(map[string]Value)
	}
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Get returns the value stored under key.
func (o *Object) Get(key string) (Value, bool) {
	v, ok := o.values[key]
	return v, ok
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	_, ok := o.values[key]
	return ok
}

// Delete removes a key, preserving the order of the remaining keys.
func (o *Object) Delete(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// Keys returns the keys in insertion order. The caller must not modify it.
func (o *Object) Keys() []string { return o.keys }

// Len returns the number of members.
func (o *Object) Len() int { return len(o.keys) }

// Parse decodes JSON text, preserving object key order.
func Parse(data []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON value")
	}
	return value, nil
}

// ParseString decodes JSON text from a string.
func ParseString(data string) (Value, error) { return Parse([]byte(data)) }

func parseValue(dec *json.Decoder) (Value, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return parseFromToken(dec, token)
}

func parseFromToken(dec *json.Decoder, token json.Token) (Value, error) {
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				value, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, value)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := Array{}
			for dec.More() {
				value, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, value)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	case json.Number:
		return numberFromJSON(t)
	case string, bool, nil:
		return t, nil
	}
	return nil, fmt.Errorf("unexpected token %v", token)
}

// numberFromJSON converts a json.Number using serde_json's rules.
func numberFromJSON(n json.Number) (Value, error) {
	text := n.String()
	if !strings.ContainsAny(text, ".eE") {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i, nil
		}
		if u, err := strconv.ParseUint(text, 10, 64); err == nil {
			return u, nil
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// MustParse panics when data is not valid JSON.
func MustParse(data string) Value {
	value, err := ParseString(data)
	if err != nil {
		panic(err)
	}
	return value
}

// Marshal encodes a value in serde_json's compact form.
func Marshal(value Value) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, value, false); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Optional distinguishes an absent value from an explicit JSON null when it is
// used as a *Optional[T] struct field:
//
//	nil            -> the field is omitted (with omitempty) or null
//	&Optional[T]{} -> explicit null
//	&Optional[T]{Value: v, Set: true} -> the value
type Optional[T any] struct {
	// Value is the wrapped value.
	Value T
	// Set reports whether Value is present.
	Set bool
}

// Some returns an Optional holding a value.
func Some[T any](value T) *Optional[T] { return &Optional[T]{Value: value, Set: true} }

// Null returns an Optional that marshals as an explicit null.
func Null[T any]() *Optional[T] { return &Optional[T]{} }

// MarshalJSON encodes the optional value.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if !o.Set {
		return []byte("null"), nil
	}
	return MarshalCompact(o.Value)
}

// UnmarshalJSON decodes the optional value.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		o.Set = false
		var zero T
		o.Value = zero
		return nil
	}
	if err := json.Unmarshal(data, &o.Value); err != nil {
		return err
	}
	o.Set = true
	return nil
}

// MarshalCompact encodes an arbitrary Go value the way serde_json does: struct
// fields in declaration order, no HTML escaping, and no escaping of U+2028 or
// U+2029.
func MarshalCompact(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	// Go escapes U+2028 and U+2029 even when HTML escaping is disabled;
	// serde_json does not.
	out = bytes.ReplaceAll(out, []byte("\\u2028"), []byte("\u2028"))
	out = bytes.ReplaceAll(out, []byte("\\u2029"), []byte("\u2029"))
	return out, nil
}

// MarshalCompactString encodes a value in serde_json's compact form.
func MarshalCompactString(value any) (string, error) {
	data, err := MarshalCompact(value)
	return string(data), err
}

// MarshalString encodes a value in serde_json's compact form.
func MarshalString(value Value) (string, error) {
	data, err := Marshal(value)
	return string(data), err
}

// MarshalPython encodes a value with Python json.dumps separators (", " and
// ": ") and Python-style negative exponents, matching
// deepseek_recipe_core::util::json_formatter::stringify_python_style.
func MarshalPython(value Value) (string, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, value, true); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// MustMarshalPython panics when the value cannot be encoded.
func MustMarshalPython(value Value) string {
	text, err := MarshalPython(value)
	if err != nil {
		panic(err)
	}
	return text
}

func writeValue(buf *bytes.Buffer, value Value, python bool) error {
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		writeString(buf, v)
	case int:
		buf.WriteString(strconv.Itoa(v))
	case int64:
		buf.WriteString(strconv.FormatInt(v, 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(v, 10))
	case float64:
		buf.WriteString(FormatFloat(v, python))
	case float32:
		buf.WriteString(FormatFloat(float64(v), python))
	case Array:
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				if python {
					buf.WriteString(", ")
				} else {
					buf.WriteByte(',')
				}
			}
			if err := writeValue(buf, item, python); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case *Object:
		buf.WriteByte('{')
		for i, key := range v.keys {
			if i > 0 {
				if python {
					buf.WriteString(", ")
				} else {
					buf.WriteByte(',')
				}
			}
			writeString(buf, key)
			if python {
				buf.WriteString(": ")
			} else {
				buf.WriteByte(':')
			}
			if err := writeValue(buf, v.values[key], python); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case Object:
		return writeValue(buf, &v, python)
	case map[string]Value:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sortStrings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				if python {
					buf.WriteString(", ")
				} else {
					buf.WriteByte(',')
				}
			}
			writeString(buf, key)
			if python {
				buf.WriteString(": ")
			} else {
				buf.WriteByte(':')
			}
			if err := writeValue(buf, v[key], python); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("jsonx: unsupported value type %T", value)
	}
	return nil
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

const hexDigits = "0123456789abcdef"

func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch c {
			case '"':
				buf.WriteString("\\\"")
			case '\\':
				buf.WriteString("\\\\")
			case '\n':
				buf.WriteString("\\n")
			case '\r':
				buf.WriteString("\\r")
			case '\t':
				buf.WriteString("\\t")
			case '\b':
				buf.WriteString("\\b")
			case '\f':
				buf.WriteString("\\f")
			default:
				if c < 0x20 {
					buf.WriteString("\\u00")
					buf.WriteByte(hexDigits[c>>4])
					buf.WriteByte(hexDigits[c&0xf])
				} else {
					buf.WriteByte(c)
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// serde_json replaces lone surrogates and invalid UTF-8 with U+FFFD.
			buf.WriteString("\uFFFD")
			i++
			continue
		}
		buf.WriteString(s[i : i+size])
		i += size
	}
	buf.WriteByte('"')
}

// FormatFloat formats a float the way serde_json (ryu) does, then applies the
// Python-style negative exponent rewrite used by stringify_python_style.
func FormatFloat(value float64, python bool) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		// serde_json refuses to serialize these; JSON has no representation.
		return "null"
	}
	text := formatRyu(value)
	if python {
		text = pythonExponent(text)
	}
	return text
}

// formatRyu renders the shortest round-trip decimal form used by ryu.
func formatRyu(value float64) string {
	negative := math.Signbit(value)
	abs := math.Abs(value)
	if abs == 0 {
		if negative {
			return "-0.0"
		}
		return "0.0"
	}

	// Shortest digits in scientific notation, e.g. "1.2345e+07".
	sci := strconv.FormatFloat(abs, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(sci, "e")
	exponent, err := strconv.Atoi(exponentText)
	if err != nil {
		return sci
	}
	digits := strings.Replace(mantissa, ".", "", 1)

	var out string
	switch {
	case exponent >= -5 && exponent < 16:
		out = fixedFromDigits(digits, exponent)
	default:
		out = scientificFromDigits(digits, exponent)
	}
	if negative {
		return "-" + out
	}
	return out
}

// fixedFromDigits writes digits with the decimal point after position exp.
func fixedFromDigits(digits string, exp int) string {
	switch {
	case exp >= 0:
		if exp+1 >= len(digits) {
			return digits + strings.Repeat("0", exp+1-len(digits)) + ".0"
		}
		return digits[:exp+1] + "." + digits[exp+1:]
	default:
		return "0." + strings.Repeat("0", -exp-1) + digits
	}
}

// scientificFromDigits writes the ryu exponent form, e.g. 1e16, 1.5e-7.
func scientificFromDigits(digits string, exp int) string {
	mantissa := digits[:1]
	if len(digits) > 1 {
		trimmed := strings.TrimRight(digits[1:], "0")
		if trimmed != "" {
			mantissa += "." + trimmed
		}
	}
	// ryu always writes the exponent sign and never pads it ("1e+16", "1.5e-7").
	sign := "+"
	if exp < 0 {
		sign, exp = "-", -exp
	}
	return mantissa + "e" + sign + strconv.Itoa(exp)
}

// pythonExponent rewrites negative exponents the way Python does, matching the
// Rust PythonStyleFormatter: a single-digit exponent gains a leading zero, and
// a mantissa of the form 0.0000ddd becomes de-0N.
func pythonExponent(text string) string {
	if !strings.Contains(text, "e") {
		sign := ""
		rest := text
		if strings.HasPrefix(rest, "-") {
			sign, rest = "-", rest[1:]
		}
		if fraction, ok := strings.CutPrefix(rest, "0."); ok {
			zeros := 0
			for zeros < len(fraction) && fraction[zeros] == '0' {
				zeros++
			}
			if zeros >= 4 && zeros < len(fraction) {
				out := sign + string(fraction[zeros])
				if remaining := fraction[zeros+1:]; remaining != "" {
					out += "." + remaining
				}
				return out + "e-" + pad2(zeros+1)
			}
		}
		return text
	}
	// serde_json only emits e- with a possibly single-digit exponent.
	index := strings.LastIndex(text, "e-")
	if index < 0 {
		return text
	}
	exponent := text[index+2:]
	if len(exponent) == 1 {
		return text[:index+2] + "0" + exponent
	}
	return text
}

func pad2(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

// UnmarshalJSON makes Object usable as a struct field type with encoding/json.
func (o *Object) UnmarshalJSON(data []byte) error {
	value, err := Parse(data)
	if err != nil {
		return err
	}
	obj, ok := value.(*Object)
	if !ok {
		return errors.New("jsonx: expected a JSON object")
	}
	*o = *obj
	return nil
}

// MarshalJSON encodes the object preserving key order.
func (o *Object) MarshalJSON() ([]byte, error) {
	return Marshal(o)
}

// String returns the compact JSON encoding.
func (o *Object) String() string {
	text, err := MarshalString(o)
	if err != nil {
		return ""
	}
	return text
}

// Clone returns a deep copy of the value.
func Clone(value Value) Value {
	switch v := value.(type) {
	case *Object:
		out := NewObject()
		for _, key := range v.keys {
			out.Set(key, Clone(v.values[key]))
		}
		return out
	case Array:
		out := make(Array, 0, len(v))
		for _, item := range v {
			out = append(out, Clone(item))
		}
		return out
	default:
		return value
	}
}
