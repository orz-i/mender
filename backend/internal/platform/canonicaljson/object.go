package canonicaljson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid canonical JSON object")

// validUnicodeEscapes rejects invalid UTF-16 escape sequences before
// encoding/json can repair them to replacement characters.
func validUnicodeEscapes(raw []byte) bool {
	inside := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil || n == 0 {
			return false
		}
		i += 4
		if n >= 0xDC00 && n <= 0xDFFF {
			return false
		}
		if n >= 0xD800 && n <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return !inside
}

func decode(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return nil, ErrInvalid
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			value := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, ErrInvalid
				}
				name, ok := key.(string)
				if !ok {
					return nil, ErrInvalid
				}
				if _, exists := value[name]; exists {
					return nil, ErrInvalid
				}
				child, err := decode(d, depth+1)
				if err != nil {
					return nil, err
				}
				value[name] = child
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, ErrInvalid
			}
			return value, nil
		case '[':
			value := []any{}
			for d.More() {
				child, err := decode(d, depth+1)
				if err != nil {
					return nil, err
				}
				value = append(value, child)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, ErrInvalid
			}
			return value, nil
		default:
			return nil, ErrInvalid
		}
	}
	return token, nil
}

func Object(raw []byte, maxBytes int) ([]byte, error) {
	if maxBytes < 2 || len(raw) == 0 || len(raw) > maxBytes || !utf8.Valid(raw) || !validUnicodeEscapes(raw) || strings.ContainsRune(string(raw), 0) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decode(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > maxBytes {
		return nil, ErrInvalid
	}
	return canonical, nil
}

func SHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
