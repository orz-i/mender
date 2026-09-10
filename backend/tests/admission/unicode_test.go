package admission_test

import (
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/input"
	"testing"
)

func TestJSONUnicodeDoesNotCollapseMalformedRequests(t *testing.T) {
	c := input.Codec{}
	q := request()
	for _, raw := range []string{`{"x":"\ud800"}`, `{"x":"\udc00"}`, `{"x":"\ud800\u0041"}`, `{"x":"\u0000"}`, `{"\u0000":1}`} {
		q.Arguments = []byte(raw)
		if _, e := c.Prepare(q); e == nil {
			t.Fatalf("accepted invalid Unicode %s", raw)
		}
	}
	q.Arguments = []byte(`{"x":"\ud83d\ude00"}`)
	a, e := c.Prepare(q)
	if e != nil {
		t.Fatal(e)
	}
	q.Arguments = []byte(`{"x":"😀"}`)
	b, e := c.Prepare(q)
	if e != nil || a != b {
		t.Fatal("valid surrogate pair normalization mismatch", e)
	}
	q.Arguments = []byte(`{"x":"\\ud800"}`)
	if _, e := c.Prepare(q); e != nil {
		t.Fatal("literal backslash sequence was rejected", e)
	}
}
