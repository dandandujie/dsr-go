package jsonx

import "testing"

func TestSpacedSeparators(t *testing.T) {
	value := MustParse(`{"type": "object", "properties": {"location": {"type": "string", "description": "The location user interested in"}}, "required": ["location"], "additionalProperties": false}`)
	want := `{"type": "object", "properties": {"location": {"type": "string", "description": "The location user interested in"}}, "required": ["location"], "additionalProperties": false}`
	if got := MustMarshalPython(value); got != want {
		t.Errorf("MarshalPython() = %s, want %s", got, want)
	}
}

func TestPreservesKeyOrder(t *testing.T) {
	value, err := ParseString(`{"b": 1, "a": 2, "c": {"z": 1, "y": 2}}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalString(value)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"b":1,"a":2,"c":{"z":1,"y":2}}`; got != want {
		t.Errorf("MarshalString() = %s, want %s", got, want)
	}
}

func TestNumbersFollowSerdeJSON(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1", "1"},
		{"-1", "-1"},
		{"1.0", "1.0"},
		{"1e5", "100000.0"},
		{"1e16", "1e+16"},
		{"1.5e-7", "1.5e-7"},
		{"0.00001", "0.00001"},
		{"-0.0", "-0.0"},
		{"1e-5", "0.00001"},
		{"18446744073709551615", "18446744073709551615"},
	}
	for _, tc := range cases {
		value, err := ParseString(tc.in)
		if err != nil {
			t.Fatalf("ParseString(%q): %v", tc.in, err)
		}
		got, err := MarshalString(value)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Marshal(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestPythonExponent(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.00001, "1e-05"},
		{0.000123, "0.000123"},
		{1.5e-7, "1.5e-07"},
		{1e-10, "1e-10"},
		{0.0001, "0.0001"},
	}
	for _, tc := range cases {
		if got := FormatFloat(tc.in, true); got != tc.want {
			t.Errorf("FormatFloat(%v, python) = %s, want %s", tc.in, got, tc.want)
		}
	}
}
