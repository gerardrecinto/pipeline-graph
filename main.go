package main

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	graphql "github.com/graph-gophers/graphql-go"
)

//go:embed schema.graphql
var schemaSDL string

const (
	maxDepth     = 6
	maxBodyBytes = 1 << 20
	maxParallel  = 100
	defaultToken = "dev-token"
)

func newSchema(s *Store) *graphql.Schema {
	return graphql.MustParseSchema(schemaSDL, &Resolver{store: s},
		graphql.MaxDepth(maxDepth), graphql.MaxParallelism(maxParallel))
}

type gqlRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

func newHandler(s *Store, schema *graphql.Schema, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST a JSON body: {\"query\": \"...\"}", http.StatusMethodNotAllowed)
			return
		}
		var req gqlRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		authed := subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
		ctx := context.WithValue(r.Context(), loadersKey, NewLoaders(s))
		ctx = context.WithValue(ctx, authedKey, authed)
		resp := schema.Exec(ctx, req.Query, req.OperationName, req.Variables)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	token := os.Getenv("API_TOKEN")
	if token == "" {
		token = defaultToken
		log.Printf("API_TOKEN not set, mutations accept %q (local use only)", defaultToken)
	}
	s := NewStore()
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newHandler(s, newSchema(s), token)))
}
