// Package strictjson decodes bounded, exact JSON object contracts.
package strictjson

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// Object requires exact member names and uniqueness. Decoding directly into a
// Go struct would accept case aliases and silently replace duplicate members.
func Object(data []byte, names ...string) (map[string]json.RawMessage, bool) {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	fields, ok := members(data, allowed)
	if !ok || len(fields) != len(names) {
		return nil, false
	}
	return fields, true
}

// Members reads one object of an open contract (for example standard metadata
// that may carry members we do not use): any names, but still unique, valid
// UTF-8 and nothing after the object.
func Members(data []byte) (map[string]json.RawMessage, bool) {
	return members(data, nil)
}

func members(data []byte, allowed map[string]bool) (map[string]json.RawMessage, bool) {
	if !utf8.Valid(data) || !validUnicodeEscapes(data) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		name, isString := token.(string)
		if err != nil || !isString || allowed != nil && !allowed[name] || fields[name] != nil {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	return fields, true
}

func StringField(fields map[string]json.RawMessage, name string) string {
	var value string
	if json.Unmarshal(fields[name], &value) != nil {
		return ""
	}
	return value
}

// encoding/json replaces unpaired UTF-16 surrogate escapes with U+FFFD. Reject
// them before decoding so malformed trust strings cannot acquire new bytes.
func validUnicodeEscapes(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil || n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		i += 4
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
