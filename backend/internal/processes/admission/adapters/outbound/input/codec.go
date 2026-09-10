package input

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Codec struct{}

// encoding/json repairs unpaired UTF-16 escapes; reject them so different invalid
// request strings cannot normalize to the same replacement character/hash.
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
		n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if e != nil || n == 0 {
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
			low, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return !inside
}

// decode preserves JSON number lexemes and rejects duplicate keys at any nesting level.
func decode(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, application.ErrInvalid
	}
	t, e := d.Token()
	if e != nil {
		return nil, application.ErrInvalid
	}
	if delimiter, ok := t.(json.Delim); ok {
		switch delimiter {
		case '{':
			m := map[string]any{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return nil, application.ErrInvalid
				}
				k, ok := key.(string)
				if !ok {
					return nil, application.ErrInvalid
				}
				if _, exists := m[k]; exists {
					return nil, application.ErrInvalid
				}
				v, e := decode(d, depth+1)
				if e != nil {
					return nil, e
				}
				m[k] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, application.ErrInvalid
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, e := decode(d, depth+1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, application.ErrInvalid
			}
			return a, nil
		default:
			return nil, application.ErrInvalid
		}
	}
	return t, nil
}
func (Codec) Prepare(q application.Request) (application.Prepared, error) {
	if len(q.Arguments) == 0 || len(q.Arguments) > 65536 || !utf8.Valid(q.Arguments) || !validUnicodeEscapes(q.Arguments) || strings.ContainsRune(string(q.Arguments), 0) {
		return application.Prepared{}, application.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(q.Arguments))
	d.UseNumber()
	value, e := decode(d, 0)
	if e != nil {
		return application.Prepared{}, e
	}
	if _, ok := value.(map[string]any); !ok {
		return application.Prepared{}, application.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return application.Prepared{}, application.ErrInvalid
	}
	canonical, e := json.Marshal(value)
	if e != nil || len(canonical) > 65536 {
		return application.Prepared{}, application.ErrInvalid
	}
	// Stable structured fields, not delimiter concatenation. Version makes future rules explicit.
	content, e := json.Marshal(struct {
		Version                                                  int
		Tool, Toolset, Connection, Budget, Period, Currency, Cap string
		Arguments                                                json.RawMessage
	}{1, q.ToolVersionID, q.ToolsetVersionID, q.ConnectionID, q.BudgetID, q.PeriodID, q.Currency, q.MaxChargeMicro, canonical})
	if e != nil {
		return application.Prepared{}, application.ErrInvalid
	}
	sum := sha256.Sum256(content)
	return application.Prepared{CanonicalArguments: string(canonical), RequestHash: hex.EncodeToString(sum[:])}, nil
}

type IDs struct{}

func (IDs) NewRunID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return "run_" + hex.EncodeToString(b[:]), nil
}
