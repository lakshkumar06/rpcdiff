package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"rpcdiff/internal/compare"
	"rpcdiff/internal/normalize"
	"rpcdiff/internal/report"
	"rpcdiff/internal/rpc"
)

type Config struct {
	Baseline  string
	Candidate string
	Requests  string
	Output    string
	HTML      string
	Timeout   time.Duration
	Workers   int
}

func Run(ctx context.Context, cfg Config) (report.Run, error) {
	if cfg.Baseline == "" || cfg.Candidate == "" {
		return report.Run{}, fmt.Errorf("baseline and candidate URLs are required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	reqs, err := rpc.LoadRequests(cfg.Requests)
	if err != nil {
		return report.Run{}, fmt.Errorf("load requests: %w", err)
	}

	client := rpc.NewClient(cfg.Timeout)
	results := make([]compare.Result, len(reqs))
	sem := make(chan struct{}, cfg.Workers)
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	for i, req := range reqs {
		i, req := i, req
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				results[i] = compare.Pair(req.Method, req.Params, rpc.CallOutcome{InvalidRequest: true, HTTPError: ctx.Err().Error()}, rpc.CallOutcome{}, false)
				results[i].Classification = compare.Inconclusive
				return
			default:
			}
			base := client.Call(ctx, cfg.Baseline, req)
			cand := client.Call(ctx, cfg.Candidate, req)
			base, _ = normalize.Outcome(req.Method, base)
			cand, nres := normalize.Outcome(req.Method, cand)
			results[i] = compare.Pair(req.Method, req.Params, base, cand, nres.Applied || normalize.Supported[req.Method])
		}()
	}
	wg.Wait()
	if firstErr != nil && ctx.Err() != nil {
		return report.Run{}, firstErr
	}

	run := report.Build(cfg.Baseline, cfg.Candidate, cfg.Timeout.String(), results)
	if cfg.Output != "" {
		if err := report.WriteJSON(cfg.Output, run); err != nil {
			return run, fmt.Errorf("write json report: %w", err)
		}
	}
	if cfg.HTML != "" {
		if err := report.WriteHTML(cfg.HTML, run); err != nil {
			return run, fmt.Errorf("write html report: %w", err)
		}
	}
	return run, nil
}
