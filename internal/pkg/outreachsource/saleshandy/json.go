package saleshandy

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// flexInt reads a number Saleshandy sends either bare or quoted ("25").
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		*f = flexInt(n)
		return nil
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		*f = flexInt(int(fl))
	}
	return nil
}

// flexString reads an id that is a hashed string on most endpoints and a number on some.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(string(b))
	return nil
}

// unwrap peels the "payload" envelopes Saleshandy nests to a different depth per endpoint.
func unwrap(raw json.RawMessage) json.RawMessage {
	for {
		t := bytes.TrimSpace(raw)
		if len(t) == 0 || t[0] != '{' {
			return t
		}
		var env struct {
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(t, &env) != nil {
			return t
		}
		p := bytes.TrimSpace(env.Payload)
		if len(p) == 0 || string(p) == "null" {
			return t
		}
		raw = p
	}
}

// decodeList reads an array found bare, inside the envelope, or under one of keys.
func decodeList(raw json.RawMessage, out any, keys ...string) error {
	raw = unwrap(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '[' {
		return json.Unmarshal(raw, out)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			v = unwrap(v)
			if len(v) > 0 && v[0] == '[' {
				return json.Unmarshal(v, out)
			}
		}
	}
	return nil
}
