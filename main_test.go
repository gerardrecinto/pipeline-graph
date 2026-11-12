package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type result struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func do(t *testing.T, query, token string) result {
	t.Helper()
	s := NewStore()
	return doWith(t, s, query, token)
}

func doWith(t *testing.T, s *Store, query, token string) result {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	newHandler(s, newSchema(s), "secret").ServeHTTP(rec, req)
	var r result
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad response %q: %v", rec.Body.String(), err)
	}
	return r
}

func TestPaginationWalksEveryRunOnce(t *testing.T) {
	s := NewStore()
	seen := map[string]bool{}
	after := ""
	for pages := 0; pages < 20; pages++ {
		arg := ""
		if after != "" {
			arg = `, after: "` + after + `"`
		}
		r := doWith(t, s, `{ runs(first: 25`+arg+`) { totalCount edges { node { id } } pageInfo { hasNextPage endCursor } } }`, "")
		if len(r.Errors) > 0 {
			t.Fatal(r.Errors)
		}
		var d struct {
			Runs struct {
				TotalCount int
				Edges      []struct{ Node struct{ ID string } }
				PageInfo   struct {
					HasNextPage bool
					EndCursor   *string
				}
			}
		}
		_ = json.Unmarshal(r.Data, &d)
		for _, e := range d.Runs.Edges {
			if seen[e.Node.ID] {
				t.Fatalf("run %s returned twice", e.Node.ID)
			}
			seen[e.Node.ID] = true
		}
		if !d.Runs.PageInfo.HasNextPage {
			if len(seen) != d.Runs.TotalCount {
				t.Fatalf("saw %d runs, totalCount %d", len(seen), d.Runs.TotalCount)
			}
			return
		}
		after = *d.Runs.PageInfo.EndCursor
	}
	t.Fatal("pagination never ended")
}

func TestStatusFilter(t *testing.T) {
	r := do(t, `{ runs(first: 100, status: FAILED) { edges { node { status } } } }`, "")
	if len(r.Errors) > 0 || !strings.Contains(string(r.Data), `"FAILED"`) || strings.Contains(string(r.Data), `"SUCCESS"`) {
		t.Fatalf("filter wrong: %s %v", r.Data, r.Errors)
	}
}

func TestBadInputIsRejected(t *testing.T) {
	for _, q := range []string{
		`{ runs(first: 0) { totalCount } }`,
		`{ runs(first: 101) { totalCount } }`,
		`{ runs(after: "not-base64!") { totalCount } }`,
	} {
		if r := do(t, q, ""); len(r.Errors) == 0 {
			t.Errorf("expected an error for %s", q)
		}
	}
}

func TestPipelineLookupsAreBatched(t *testing.T) {
	s := NewStore()
	l := NewLoaders(s)
	body, _ := json.Marshal(map[string]any{"query": `{ runs(first: 40) { edges { node { id pipeline { name service { name } } } } } }`})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), loadersKey, l)
	resp := newSchema(s).Exec(ctx, string(mustQuery(body)), "", nil)
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors)
	}
	if got := l.PipelineBatches.Load(); got != 1 {
		t.Fatalf("40 runs caused %d pipeline batches, want 1", got)
	}
}

func mustQuery(body []byte) []byte {
	var m map[string]string
	_ = json.Unmarshal(body, &m)
	return []byte(m["query"])
}

func TestDepthLimit(t *testing.T) {
	q := `{ services { pipelines { service { pipelines { service { pipelines { service { name } } } } } } } }`
	r := do(t, q, "")
	if len(r.Errors) == 0 || !strings.Contains(r.Errors[0].Message, "depth") {
		t.Fatalf("expected depth error, got %v", r.Errors)
	}
}

func TestMutationNeedsToken(t *testing.T) {
	const m = `mutation { triggerRun(pipelineId: "api-build", branch: "main") { id status } }`
	if r := do(t, m, ""); len(r.Errors) == 0 {
		t.Fatal("mutation without a token succeeded")
	}
	if r := do(t, m, "wrong"); len(r.Errors) == 0 {
		t.Fatal("mutation with a wrong token succeeded")
	}
	r := do(t, m, "secret")
	if len(r.Errors) > 0 || !strings.Contains(string(r.Data), `"RUNNING"`) {
		t.Fatalf("authorised mutation failed: %s %v", r.Data, r.Errors)
	}
}

func TestMutationValidation(t *testing.T) {
	for _, m := range []string{
		`mutation { triggerRun(pipelineId: "nope", branch: "main") { id } }`,
		`mutation { triggerRun(pipelineId: "api-build", branch: "bad branch;rm") { id } }`,
	} {
		if r := do(t, m, "secret"); len(r.Errors) == 0 {
			t.Errorf("expected validation error for %s", m)
		}
	}
}

func TestNewRunAppearsInQuery(t *testing.T) {
	s := NewStore()
	doWith(t, s, `mutation { triggerRun(pipelineId: "web-deploy", branch: "dev") { id } }`, "secret")
	r := doWith(t, s, `{ runs(first: 1, status: RUNNING) { totalCount } }`, "")
	if !strings.Contains(string(r.Data), `"totalCount":13`) {
		t.Fatalf("new run missing: %s", r.Data)
	}
}
