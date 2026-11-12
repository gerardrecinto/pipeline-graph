package main

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
		if len(resp.Errors) > 0 {
			gqlErrors.Inc()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func routes(s *Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", instrument(newHandler(s, newSchema(s), token)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.Handle("/metrics", metricsHandler())
	return mux
}

// run serves until ctx is cancelled, then drains in-flight requests.
func run(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	token := os.Getenv("API_TOKEN")
	if token == "" {
		token = defaultToken
		log.Warn("API_TOKEN not set, mutations accept the default token (local use only)")
	}
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Error("listen failed", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Info("listening", "addr", ln.Addr().String())
	if err := run(ctx, ln, routes(NewStore(), token)); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
	log.Info("shut down cleanly")
}
