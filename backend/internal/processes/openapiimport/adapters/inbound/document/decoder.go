package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/orz-i/mender/backend/internal/processes/openapiimport/application"
)

const maxDocumentBytes = 1 << 20

var ErrInvalidDocument = errors.New("invalid OpenAPI document")

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return ErrInvalidDocument
				}
				seen[key] = true
				if err = walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err = walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return ErrInvalidDocument
		}
	}
	if err := walk(); err != nil {
		return ErrInvalidDocument
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidDocument
	}
	return nil
}

func Parse(raw []byte) (application.Result, error) {
	if len(raw) < 2 || len(raw) > maxDocumentBytes || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return application.Result{}, ErrInvalidDocument
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 {
		return application.Result{}, ErrInvalidDocument
	}
	var root map[string]any
	if trimmed[0] == '{' {
		if rejectDuplicateJSONKeys(trimmed) != nil || json.Unmarshal(trimmed, &root) != nil || root == nil {
			return application.Result{}, ErrInvalidDocument
		}
	} else {
		if yaml.Unmarshal(trimmed, &root) != nil || root == nil {
			return application.Result{}, ErrInvalidDocument
		}
	}
	return application.Analyze(root), nil
}
