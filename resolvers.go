package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	graphql "github.com/graph-gophers/graphql-go"
)

var (
	errNotFound     = errors.New("not found")
	errUnauthorized = errors.New("unauthorized: send Authorization: Bearer <token>")
	branchRe        = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
)

const maxPage = 100

type Resolver struct{ store *Store }

func (r *Resolver) Services() []*serviceResolver {
	var out []*serviceResolver
	for _, s := range r.store.Services() {
		out = append(out, &serviceResolver{r.store, s})
	}
	return out
}

func (r *Resolver) Service(args struct{ ID graphql.ID }) *serviceResolver {
	s, ok := r.store.Service(string(args.ID))
	if !ok {
		return nil
	}
	return &serviceResolver{r.store, s}
}

func (r *Resolver) Runs(args struct {
	First  int32
	After  *string
	Status *string
}) (*runConnection, error) {
	limit := int(args.First)
	if limit < 1 || limit > maxPage {
		return nil, fmt.Errorf("first must be between 1 and %d", maxPage)
	}
	after := 0
	if args.After != nil {
		n, err := decodeCursor(*args.After)
		if err != nil {
			return nil, err
		}
		after = n
	}
	status := ""
	if args.Status != nil {
		status = *args.Status
	}
	page, hasNext, total := r.store.RunsPage(after, limit, status)
	c := &runConnection{total: int32(total), hasNext: hasNext}
	for _, run := range page {
		c.edges = append(c.edges, &runEdge{&runResolver{r.store, run}})
	}
	return c, nil
}

func (r *Resolver) TriggerRun(ctx context.Context, args struct {
	PipelineID graphql.ID
	Branch     string
}) (*runResolver, error) {
	if authed, _ := ctx.Value(authedKey).(bool); !authed {
		return nil, errUnauthorized
	}
	if !branchRe.MatchString(args.Branch) {
		return nil, errors.New("branch must be 1-100 characters of letters, digits, . _ / -")
	}
	if _, ok := r.store.Pipeline(string(args.PipelineID)); !ok {
		return nil, fmt.Errorf("pipeline %q: %w", args.PipelineID, errNotFound)
	}
	return &runResolver{r.store, r.store.AddRun(string(args.PipelineID), args.Branch)}, nil
}

func encodeCursor(id int) string {
	return base64.StdEncoding.EncodeToString([]byte("run:" + strconv.Itoa(id)))
}

func decodeCursor(c string) (int, error) {
	b, err := base64.StdEncoding.DecodeString(c)
	if err != nil || !strings.HasPrefix(string(b), "run:") {
		return 0, errors.New("invalid cursor")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(b), "run:"))
	if err != nil || n < 0 {
		return 0, errors.New("invalid cursor")
	}
	return n, nil
}

type serviceResolver struct {
	store *Store
	s     Service
}

func (s *serviceResolver) ID() graphql.ID { return graphql.ID(s.s.ID) }
func (s *serviceResolver) Name() string   { return s.s.Name }
func (s *serviceResolver) Pipelines() []*pipelineResolver {
	var out []*pipelineResolver
	for _, p := range s.store.PipelinesOf(s.s.ID) {
		out = append(out, &pipelineResolver{s.store, p})
	}
	return out
}

type pipelineResolver struct {
	store *Store
	p     Pipeline
}

func (p *pipelineResolver) ID() graphql.ID { return graphql.ID(p.p.ID) }
func (p *pipelineResolver) Name() string   { return p.p.Name }
func (p *pipelineResolver) SuccessRate() float64 {
	return p.store.SuccessRate(p.p.ID)
}
func (p *pipelineResolver) Service(ctx context.Context) (*serviceResolver, error) {
	s, err := loadersFrom(ctx).Services.Load(ctx, p.p.ServiceID)()
	if err != nil {
		return nil, err
	}
	return &serviceResolver{p.store, s}, nil
}

type runResolver struct {
	store *Store
	r     Run
}

func (r *runResolver) ID() graphql.ID         { return graphql.ID(strconv.Itoa(r.r.ID)) }
func (r *runResolver) Status() string         { return r.r.Status }
func (r *runResolver) Branch() string         { return r.r.Branch }
func (r *runResolver) DurationSeconds() int32 { return r.r.DurationSec }
func (r *runResolver) Pipeline(ctx context.Context) (*pipelineResolver, error) {
	p, err := loadersFrom(ctx).Pipelines.Load(ctx, r.r.PipelineID)()
	if err != nil {
		return nil, err
	}
	return &pipelineResolver{r.store, p}, nil
}

type runConnection struct {
	total   int32
	hasNext bool
	edges   []*runEdge
}

func (c *runConnection) TotalCount() int32   { return c.total }
func (c *runConnection) Edges() []*runEdge   { return c.edges }
func (c *runConnection) PageInfo() *pageInfo { return &pageInfo{c.hasNext, c.edges} }

type runEdge struct{ node *runResolver }

func (e *runEdge) Cursor() string     { return encodeCursor(e.node.r.ID) }
func (e *runEdge) Node() *runResolver { return e.node }

type pageInfo struct {
	hasNext bool
	edges   []*runEdge
}

func (p *pageInfo) HasNextPage() bool { return p.hasNext }
func (p *pageInfo) EndCursor() *string {
	if len(p.edges) == 0 {
		return nil
	}
	c := p.edges[len(p.edges)-1].Cursor()
	return &c
}
