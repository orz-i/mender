package admission_test

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/input"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"testing"
	"time"
)

type advancingClock struct{ calls int }

func (c *advancingClock) Now() time.Time {
	c.calls++
	if c.calls >= 3 {
		return moment.Add(time.Hour)
	}
	return moment
}
func TestPlanExpiryAfterReserveNeverCreatesRun(t *testing.T) {
	u := &unit{scope: &scope{}}
	s, e := application.New(authFunc(allow), resolverFunc(resolve), input.Codec{}, ids{}, &advancingClock{}, u)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Admit(context.Background(), who, request())
	if !errors.Is(e, application.ErrInvalid) || r.RunID != "" {
		t.Fatal(r, e)
	}
	if len(u.scope.ops) != 2 || u.scope.ops[1] != "reserve" {
		t.Fatal("expired plan continued persistence", u.scope.ops)
	}
}
