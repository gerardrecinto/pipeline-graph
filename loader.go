package main

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/graph-gophers/dataloader/v7"
)

type ctxKey string

const (
	loadersKey ctxKey = "loaders"
	authedKey  ctxKey = "authed"
)

// Loaders batch and cache lookups for one request, so a list of N runs
// costs one pipeline lookup instead of N.
type Loaders struct {
	Pipelines       *dataloader.Loader[string, Pipeline]
	Services        *dataloader.Loader[string, Service]
	PipelineBatches atomic.Int64
}

func NewLoaders(s *Store) *Loaders {
	l := &Loaders{}
	wait := dataloader.WithWait[string, Pipeline](5 * time.Millisecond)
	l.Pipelines = dataloader.NewBatchedLoader(func(_ context.Context, keys []string) []*dataloader.Result[Pipeline] {
		l.PipelineBatches.Add(1)
		out := make([]*dataloader.Result[Pipeline], len(keys))
		for i, k := range keys {
			p, ok := s.Pipeline(k)
			if !ok {
				out[i] = &dataloader.Result[Pipeline]{Error: errNotFound}
				continue
			}
			out[i] = &dataloader.Result[Pipeline]{Data: p}
		}
		return out
	}, wait)
	l.Services = dataloader.NewBatchedLoader(func(_ context.Context, keys []string) []*dataloader.Result[Service] {
		out := make([]*dataloader.Result[Service], len(keys))
		for i, k := range keys {
			v, ok := s.Service(k)
			if !ok {
				out[i] = &dataloader.Result[Service]{Error: errNotFound}
				continue
			}
			out[i] = &dataloader.Result[Service]{Data: v}
		}
		return out
	}, dataloader.WithWait[string, Service](5*time.Millisecond))
	return l
}

func loadersFrom(ctx context.Context) *Loaders { return ctx.Value(loadersKey).(*Loaders) }
