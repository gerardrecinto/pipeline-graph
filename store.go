package main

import (
	"fmt"
	"sync"
)

type Service struct{ ID, Name string }

type Pipeline struct{ ID, ServiceID, Name string }

type Run struct {
	ID          int
	PipelineID  string
	Status      string
	Branch      string
	DurationSec int32
}

// Store is an in-memory CI/CD data set: services own pipelines, pipelines own runs.
type Store struct {
	mu        sync.RWMutex
	services  []Service
	pipelines []Pipeline
	runs      []Run
}

func NewStore() *Store {
	s := &Store{}
	for i, name := range []string{"api", "web", "data"} {
		sid := fmt.Sprint(i + 1)
		s.services = append(s.services, Service{ID: sid, Name: name})
		for _, p := range []string{"build", "deploy"} {
			s.pipelines = append(s.pipelines, Pipeline{ID: fmt.Sprintf("%s-%s", name, p), ServiceID: sid, Name: p})
		}
	}
	statuses := []string{"SUCCESS", "SUCCESS", "FAILED", "SUCCESS", "RUNNING"}
	branches := []string{"main", "main", "dev"}
	for i := 1; i <= 60; i++ {
		s.runs = append(s.runs, Run{
			ID:          i,
			PipelineID:  s.pipelines[i%len(s.pipelines)].ID,
			Status:      statuses[i%len(statuses)],
			Branch:      branches[i%len(branches)],
			DurationSec: int32(60 + (i*37)%540),
		})
	}
	return s
}

func (s *Store) Services() []Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Service(nil), s.services...)
}

func (s *Store) Service(id string) (Service, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.services {
		if v.ID == id {
			return v, true
		}
	}
	return Service{}, false
}

func (s *Store) Pipeline(id string) (Pipeline, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.pipelines {
		if p.ID == id {
			return p, true
		}
	}
	return Pipeline{}, false
}

func (s *Store) PipelinesOf(serviceID string) []Pipeline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Pipeline
	for _, p := range s.pipelines {
		if p.ServiceID == serviceID {
			out = append(out, p)
		}
	}
	return out
}

// RunsPage returns up to limit runs with ID > after, optionally filtered by status.
func (s *Store) RunsPage(after, limit int, status string) (page []Run, hasNext bool, total int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.runs {
		if status != "" && r.Status != status {
			continue
		}
		total++
		if r.ID <= after {
			continue
		}
		if len(page) < limit {
			page = append(page, r)
		} else {
			hasNext = true
		}
	}
	return page, hasNext, total
}

func (s *Store) SuccessRate(pipelineID string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var done, ok int
	for _, r := range s.runs {
		if r.PipelineID != pipelineID || r.Status == "RUNNING" {
			continue
		}
		done++
		if r.Status == "SUCCESS" {
			ok++
		}
	}
	if done == 0 {
		return 0
	}
	return float64(ok) / float64(done)
}

func (s *Store) AddRun(pipelineID, branch string) Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := Run{ID: len(s.runs) + 1, PipelineID: pipelineID, Status: "RUNNING", Branch: branch}
	s.runs = append(s.runs, r)
	return r
}
