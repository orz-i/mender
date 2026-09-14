//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	objectfs "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/objectfs"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	executionapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	executiondomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type artifactObjectClock struct{ at time.Time }

func (c artifactObjectClock) Now() time.Time { return c.at }

func exerciseArtifacts(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN, otherWorkspaceKey string) {
	t.Helper()

	var artifactID, kind, mediaType, content string
	var size int64
	var createdAt time.Time
	must(t, owner.QueryRow(ctx, `SELECT id,kind,media_type,octet_length(content_json::text),content_json::text,created_at FROM execution.artifacts WHERE workspace_id='ws_result' AND run_id='run_result_success'`).Scan(&artifactID, &kind, &mediaType, &size, &content, &createdAt))
	if artifactID != "art_run_result_success" || kind != "provider_result" || mediaType != "application/json" || content != `{"answer": 42}` && content != `{"answer":42}` || size < 1 || size > 1<<20 {
		t.Fatal("unexpected durable provider result artifact", artifactID, kind, mediaType, size, content)
	}
	var artifacts, succeeded int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.artifacts`).Scan(&artifacts))
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.provider_observations WHERE state='succeeded'`).Scan(&succeeded))
	if artifacts != succeeded || artifacts < 1 {
		t.Fatal("Artifact/result cardinality diverged", artifacts, succeeded)
	}
	var invalid int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.artifacts a JOIN execution.provider_observations p ON (p.workspace_id,p.run_id,p.observation_id)=(a.workspace_id,a.run_id,a.source_observation_id) WHERE p.state<>'succeeded'`).Scan(&invalid))
	if invalid != 0 {
		t.Fatal("non-success Provider observation produced Artifact")
	}

	repo := runpg.New(runtime)
	items, err := repo.FetchArtifacts(ctx, ports.WorkspaceID("ws_result"), ports.RunID("run_result_success"))
	must(t, err)
	if len(items) != 1 || items[0].ArtifactID != artifactID || items[0].SizeBytes != size {
		t.Fatal("runtime Artifact projection mismatch", items)
	}
	record, err := repo.FetchArtifact(ctx, "ws_result", "run_result_success", artifactID)
	must(t, err)
	if record.ContentJSON != content {
		t.Fatal("runtime Artifact content mismatch")
	}
	if hidden, err := repo.FetchArtifacts(ctx, "ws_a", "run_result_success"); err == nil || hidden != nil {
		t.Fatal("cross-tenant Artifact lookup escaped Run existence/RLS", hidden, err)
	}

	t.Run("runtime Artifact grant is read-only and hides source observation linkage", func(t *testing.T) {
		tx, err := runtime.Begin(ctx)
		must(t, err)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_result',true)`)
		must(t, err)
		if err = tx.QueryRow(ctx, `SELECT content_json::text FROM execution.artifacts WHERE workspace_id='ws_result' AND id='art_run_result_success'`).Scan(&content); err != nil {
			t.Fatal("runtime cannot read allowed Artifact content", err)
		}
		if err = tx.QueryRow(ctx, `SELECT source_observation_id FROM execution.artifacts WHERE workspace_id='ws_result' AND id='art_run_result_success'`).Scan(&content); err == nil {
			t.Fatal("runtime can read internal source observation linkage")
		}
		if _, err = tx.Exec(ctx, `UPDATE execution.artifacts SET media_type='application/json' WHERE workspace_id='ws_result'`); err == nil {
			t.Fatal("runtime can mutate immutable Artifact")
		}
	})

	// Build a real machine credential for the provider-result fixture without
	// changing the execution evidence used by earlier integration slices.
	_, err = owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES('ws_result',$1) ON CONFLICT(id) DO NOTHING`, createdAt.Add(-time.Minute))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.service_accounts(workspace_id,id,created_at) VALUES('ws_result','sa_result_reader',$1) ON CONFLICT(workspace_id,id) DO NOTHING`, createdAt.Add(-time.Minute))
	must(t, err)
	raw, keyID, digest, err := (keycodec.Codec{}).Generate()
	must(t, err)
	must(t, identitypg.New(owner).Provision(ctx, identitydomain.Credential{ID: keyID, WorkspaceID: "ws_result", SubjectID: "sa_result_reader", Digest: digest, Scopes: []string{"run:read"}, CreatedAt: createdAt.Add(-time.Minute), ExpiresAt: time.Now().UTC().Add(time.Hour)}))

	h, closeAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, RunReadAPIEnabled: true, DatabaseURL: runtimeDSN, CursorSigningKey: bytes.Repeat([]byte{7}, 32)})
	must(t, err)
	defer closeAPI()
	request := func(key, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	base := "/api/v1/workspaces/ws_result/runs/run_result_success/artifacts"
	w := request(raw, base)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"artifact_id":"art_run_result_success"`) || strings.Contains(w.Body.String(), `"content"`) {
		t.Fatal("Artifact list response mismatch", w.Code, w.Body.String())
	}
	w = request(raw, base+"/art_run_result_success")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"content":{"answer":42}`) {
		t.Fatal("Artifact detail response mismatch", w.Code, w.Body.String())
	}
	for _, internal := range []string{"provider_request_id", "external_task_id", "source_observation_id", "canonical_arguments", digest} {
		if strings.Contains(w.Body.String(), internal) {
			t.Fatal("Artifact API leaked internal/provider evidence", internal)
		}
	}
	if w = request(otherWorkspaceKey, base); w.Code != 403 {
		t.Fatal("other Workspace credential reached Artifact", w.Code, w.Body.String())
	}
	if w = request(raw, base+"/missing"); w.Code != 404 {
		t.Fatal("missing Artifact status", w.Code)
	}

	// Provider observation replay in the previous slice must never duplicate the Artifact.
	var count int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.artifacts WHERE workspace_id='ws_result' AND run_id='run_result_success' AND kind='provider_result'`).Scan(&count))
	if count != 1 {
		t.Fatal("terminal Provider replay duplicated Artifact", count)
	}

	t.Run("large Artifact materializes through restricted object role", func(t *testing.T) {
		materializerDB, materializerDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_artifact_materializer_", migrations.GrantArtifactMaterializer)
		must(t, database.ArtifactMaterializerRole(ctx, materializerDB))
		if database.ArtifactMaterializerRole(ctx, owner) == nil || database.ArtifactMaterializerRole(ctx, runtime) == nil {
			t.Fatal("owner/runtime role accepted as artifact materializer")
		}
		objectReaderDB, objectReaderDSN := openTemporaryRoleWithDSN(t, ctx, owner, runtimeDSN, "mender_artifact_object_reader_", migrations.GrantArtifactObjectReader)
		must(t, database.ArtifactObjectReaderRole(ctx, objectReaderDB))
		if database.ArtifactObjectReaderRole(ctx, owner) == nil || database.ArtifactObjectReaderRole(ctx, runtime) == nil || database.ArtifactObjectReaderRole(ctx, materializerDB) == nil {
			t.Fatal("owner/runtime/materializer role accepted as artifact object reader")
		}

		largeContent := `{"answer":42,"padding":"` + strings.Repeat("x", (256<<10)+1024) + `"}`
		if len(largeContent) <= int(executionapp.ArtifactObjectThresholdBytes) || len(largeContent) > 1<<20 {
			t.Fatal("large Artifact fixture is outside materialization bounds", len(largeContent))
		}
		tx, err := owner.Begin(ctx)
		must(t, err)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_result',true)`)
		must(t, err)
		_, err = tx.Exec(ctx, `UPDATE execution.provider_observations SET result_json=$1::jsonb WHERE workspace_id='ws_result' AND run_id='run_result_success' AND state='succeeded'`, largeContent)
		must(t, err)
		_, err = tx.Exec(ctx, `UPDATE execution.artifacts SET content_json=$1::jsonb WHERE workspace_id='ws_result' AND run_id='run_result_success' AND id='art_run_result_success'`, largeContent)
		must(t, err)
		must(t, tx.Commit(ctx))

		objectRoot := t.TempDir()
		store, err := objectfs.New(objectRoot)
		must(t, err)
		materializerRepo := runpg.New(materializerDB)
		candidate, found, err := materializerRepo.NextArtifactObjectCandidate(ctx, "ws_result")
		if err != nil || !found {
			t.Fatal("artifact object candidate unavailable", found, err)
		}
		if !candidate.Valid() || candidate.ArtifactID != "art_run_result_success" {
			t.Fatal("artifact object candidate drift", candidate.ArtifactID, len(candidate.ContentJSON), candidate.MediaType)
		}
		storedPreview, err := store.PutArtifactObject(ctx, candidate)
		if err != nil {
			t.Fatal("filesystem object write failed", err)
		}
		if storedPreview.SizeBytes != int64(len(candidate.ContentJSON)) {
			t.Fatal("filesystem object size drift", storedPreview.SizeBytes, len(candidate.ContentJSON))
		}
		reviewedRuntime, closeRuntime, err := bootstrap.BuildArtifactObjectRuntime(ctx, bootstrap.ArtifactObjectRuntimeConfig{DatabaseURL: materializerDSN, Root: objectRoot, Retention: 24 * time.Hour})
		must(t, err)
		defer closeRuntime()
		cycle, err := reviewedRuntime.CycleOne(ctx, executiondomain.WorkspaceID("ws_result"))
		must(t, err)
		if !cycle.Materialized || cycle.Expired {
			t.Fatal("reviewed Artifact object cycle did not materialize exactly one durable object", cycle)
		}
		object, err := materializerRepo.FindArtifactObject(ctx, "ws_result", "run_result_success", "art_run_result_success")
		must(t, err)
		if object.Validate() != nil || object.ArtifactID != "art_run_result_success" || object.SizeBytes != storedPreview.SizeBytes || object.ContentSHA256 != storedPreview.ContentSHA256 || object.ObjectKey != storedPreview.ObjectKey || object.State != executiondomain.ArtifactObjectAvailable || object.ExpiresAt.Sub(object.MaterializedAt) != 24*time.Hour {
			t.Fatal("materialized object metadata drift", object)
		}
		cycle, err = reviewedRuntime.CycleOne(ctx, "ws_result")
		must(t, err)
		if cycle.Materialized || cycle.Expired {
			t.Fatal("reviewed Artifact object replay did not converge", cycle)
		}

		var storedKey, storedSHA, storedState string
		var storedSize int64
		must(t, owner.QueryRow(ctx, `SELECT object_key,content_sha256,size_bytes,state FROM execution.artifact_objects WHERE workspace_id='ws_result' AND artifact_id='art_run_result_success'`).Scan(&storedKey, &storedSHA, &storedSize, &storedState))
		if storedKey != object.ObjectKey || storedSHA != object.ContentSHA256 || storedSize != object.SizeBytes || storedState != "available" {
			t.Fatal("durable artifact object metadata drift", storedKey, storedSHA, storedSize, storedState)
		}

		roleTx, err := materializerDB.Begin(ctx)
		must(t, err)
		defer func() { _ = roleTx.Rollback(context.Background()) }()
		_, err = roleTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_result',true)`)
		must(t, err)
		if err = roleTx.QueryRow(ctx, `SELECT object_key FROM execution.artifact_objects WHERE workspace_id='ws_result' AND artifact_id='art_run_result_success'`).Scan(&storedKey); err != nil {
			t.Fatal("materializer cannot inspect its object metadata", err)
		}
		if err = roleTx.QueryRow(ctx, `SELECT source_observation_id FROM execution.artifacts WHERE workspace_id='ws_result' AND id='art_run_result_success'`).Scan(&storedKey); err == nil {
			t.Fatal("materializer can read source observation linkage")
		}
		if err = roleTx.QueryRow(ctx, `SELECT observation_id FROM execution.provider_observations WHERE workspace_id='ws_result' LIMIT 1`).Scan(&storedKey); err == nil {
			t.Fatal("materializer can read provider observations")
		}
		if err = roleTx.QueryRow(ctx, `SELECT id FROM identity.workspaces LIMIT 1`).Scan(&storedKey); err == nil {
			t.Fatal("materializer can reach identity schema")
		}

		readerTx, err := objectReaderDB.Begin(ctx)
		must(t, err)
		defer func() { _ = readerTx.Rollback(context.Background()) }()
		_, err = readerTx.Exec(ctx, `SELECT set_config('mender.workspace_id','ws_result',true)`)
		must(t, err)
		if err = readerTx.QueryRow(ctx, `SELECT state FROM execution.artifact_objects WHERE workspace_id='ws_result' AND artifact_id='art_run_result_success'`).Scan(&storedState); err != nil {
			t.Fatal("artifact object reader cannot inspect safe object metadata", err)
		}
		if err = readerTx.QueryRow(ctx, `SELECT content_json::text FROM execution.artifacts WHERE workspace_id='ws_result' AND id='art_run_result_success'`).Scan(&storedState); err == nil {
			t.Fatal("artifact object reader can read inline Artifact content")
		}

		keyFile := filepath.Join(t.TempDir(), "artifact-object-signing.key")
		must(t, os.WriteFile(keyFile, bytes.Repeat([]byte{11}, 32), 0o600))
		objectAPI, closeObjectAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{
			RunAPIEnabled: true, RunReadAPIEnabled: true, DatabaseURL: runtimeDSN, CursorSigningKey: bytes.Repeat([]byte{7}, 32),
			ArtifactObjectReadEnabled: true, ArtifactObjectDatabaseURL: objectReaderDSN, ArtifactObjectRoot: objectRoot,
			ArtifactObjectSigningKeyFile: keyFile, ArtifactObjectCapabilityTTL: time.Minute,
		})
		must(t, err)
		defer closeObjectAPI()
		objectRequest := func(method, key, path string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, nil)
			if key != "" {
				r.Header.Set("Authorization", "Bearer "+key)
			}
			w := httptest.NewRecorder()
			objectAPI.ServeHTTP(w, r)
			return w
		}
		objectBase := "/api/v1/workspaces/ws_result/runs/run_result_success/artifacts/art_run_result_success/object"
		statusResponse := objectRequest(http.MethodGet, raw, objectBase)
		if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"state":"available"`) || strings.Contains(statusResponse.Body.String(), "object_key") || strings.Contains(statusResponse.Body.String(), storedKey) {
			t.Fatal("Artifact object status leaked routing metadata or lost availability", statusResponse.Code, statusResponse.Body.String())
		}
		grantResponse := objectRequest(http.MethodPost, raw, objectBase+"-capability")
		if grantResponse.Code != http.StatusCreated || strings.Contains(grantResponse.Body.String(), "object_key") || strings.Contains(grantResponse.Body.String(), storedKey) {
			t.Fatal("Artifact object capability response drift", grantResponse.Code, grantResponse.Body.String())
		}
		var grant struct {
			Data struct {
				URL       string    `json:"url"`
				ExpiresAt time.Time `json:"expires_at"`
			} `json:"data"`
		}
		must(t, json.Unmarshal(grantResponse.Body.Bytes(), &grant))
		if !strings.HasPrefix(grant.Data.URL, "/api/v1/artifact-objects/content?cap=") || grant.Data.ExpiresAt.IsZero() || strings.Contains(grant.Data.URL, storedKey) {
			t.Fatal("signed object URL contract drift", grant.Data.URL, grant.Data.ExpiresAt)
		}
		signedResponse := objectRequest(http.MethodGet, "", grant.Data.URL)
		if signedResponse.Code != http.StatusOK || signedResponse.Header().Get("Cache-Control") != "private, no-store" || signedResponse.Header().Get("Referrer-Policy") != "no-referrer" || signedResponse.Body.String() != candidate.ContentJSON {
			t.Fatal("signed Artifact object read mismatch", signedResponse.Code, signedResponse.Header(), len(signedResponse.Body.String()))
		}
		if tampered := objectRequest(http.MethodGet, "", grant.Data.URL+"x"); tampered.Code != http.StatusNotFound {
			t.Fatal("tampered object capability accepted", tampered.Code, tampered.Body.String())
		}
		if other := objectRequest(http.MethodPost, otherWorkspaceKey, objectBase+"-capability"); other.Code != http.StatusForbidden {
			t.Fatal("cross-workspace credential issued object capability", other.Code, other.Body.String())
		}
		if missingAuth := objectRequest(http.MethodPost, "", objectBase+"-capability"); missingAuth.Code != http.StatusUnauthorized {
			t.Fatal("anonymous object capability issuance accepted", missingAuth.Code)
		}

		cleanup, err := executionapp.NewArtifactObjectMaterializer(materializerRepo, store, artifactObjectClock{at: object.ExpiresAt.Add(time.Minute)}, 24*time.Hour)
		must(t, err)
		expired, err := cleanup.ExpireOne(ctx, "ws_result")
		must(t, err)
		if expired.State != executiondomain.ArtifactObjectExpired || expired.DeletedAt.Before(expired.ExpiresAt) {
			t.Fatal("Artifact object cleanup did not converge", expired)
		}
		statusResponse = objectRequest(http.MethodGet, raw, objectBase)
		if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"state":"expired"`) {
			t.Fatal("expired object status was not server-owned", statusResponse.Code, statusResponse.Body.String())
		}
		if expiredRead := objectRequest(http.MethodGet, "", grant.Data.URL); expiredRead.Code != http.StatusGone {
			t.Fatal("expired signed object capability remained readable", expiredRead.Code, expiredRead.Body.String())
		}
	})
}
