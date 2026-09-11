package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestArtifactForProviderResultRequiresSucceededBoundedJSON(t *testing.T) {
	at := time.Date(2026, 9, 11, 9, 30, 0, 0, time.UTC)
	base := ProviderObservation{WorkspaceID: "ws_a", RunID: "run_a", ObservationID: "obs_a", AttemptNo: 1, ProviderID: "provider_a", ProviderRequestID: "req/a", State: ProviderSucceeded, ResultJSON: `{"ok":true}`, ObservedAt: at}
	a, err := ArtifactForProviderResult(base)
	if err != nil || a.ID != "art_run_a" || a.SourceObservationID != "obs_a" || a.ContentJSON != base.ResultJSON || !a.CreatedAt.Equal(at) || a.Validate() != nil {
		t.Fatal(a, err)
	}

	for _, mutate := range []func(*ProviderObservation){
		func(o *ProviderObservation) { o.State = ProviderFailed; o.ResultJSON = ""; o.ErrorCode = "failed" },
		func(o *ProviderObservation) { o.State = ProviderCanceled; o.ResultJSON = "" },
		func(o *ProviderObservation) { o.ResultJSON = "{" },
		func(o *ProviderObservation) { o.ResultJSON = `"` + strings.Repeat("x", 1<<20) + `"` },
	} {
		o := base
		mutate(&o)
		if _, err := ArtifactForProviderResult(o); !errors.Is(err, ErrInvalidArtifact) {
			t.Fatal("invalid provider result produced artifact", o.State, err)
		}
	}
}
