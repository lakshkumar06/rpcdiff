package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"rpcdiff/internal/app"
	"rpcdiff/internal/fixtures"
	"rpcdiff/internal/report"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "compare":
		if err := runCompare(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "demo":
		if err := runDemo(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `rpcdiff — compare two Ethereum-compatible JSON-RPC endpoints

Usage:
  rpcdiff compare --baseline URL --candidate URL --requests FILE --output FILE [flags]
  rpcdiff demo [--output FILE] [--html FILE]

compare flags:
  --baseline URL     Baseline JSON-RPC HTTP endpoint
  --candidate URL    Candidate JSON-RPC HTTP endpoint
  --requests FILE    JSON array of JSON-RPC requests
  --output FILE      Machine-readable JSON report path
  --html FILE        Optional HTML report path
  --timeout DURATION Per-request timeout (default 5s)
  --workers N        Concurrent request workers (default 4)

demo starts two in-process fake RPC servers and compares examples/requests.json.
`)
}

func runCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	baseline := fs.String("baseline", "", "baseline RPC URL")
	candidate := fs.String("candidate", "", "candidate RPC URL")
	requests := fs.String("requests", "", "JSON-RPC request file")
	output := fs.String("output", "report.json", "JSON report output")
	htmlPath := fs.String("html", "", "optional HTML report output")
	timeout := fs.Duration("timeout", 5*time.Second, "per-request timeout")
	workers := fs.Int("workers", 4, "concurrent workers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *baseline == "" || *candidate == "" || *requests == "" {
		return fmt.Errorf(" --baseline, --candidate, and --requests are required")
	}
	run, err := app.Run(context.Background(), app.Config{
		Baseline:  *baseline,
		Candidate: *candidate,
		Requests:  *requests,
		Output:    *output,
		HTML:      *htmlPath,
		Timeout:   *timeout,
		Workers:   *workers,
	})
	if err != nil {
		return err
	}
	report.PrintSummary(os.Stdout, run)
	fmt.Fprintf(os.Stdout, "wrote %s\n", *output)
	if *htmlPath != "" {
		fmt.Fprintf(os.Stdout, "wrote %s\n", *htmlPath)
	}
	return nil
}

func runDemo(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	output := fs.String("output", "report.json", "JSON report output")
	htmlPath := fs.String("html", "report.html", "HTML report output")
	timeout := fs.Duration("timeout", 800*time.Millisecond, "per-request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	baseSrv := &http.Server{Addr: "127.0.0.1:0", Handler: fixtures.Handler(fixtures.Baseline)}
	candSrv := &http.Server{Addr: "127.0.0.1:0", Handler: fixtures.Handler(fixtures.Candidate)}
	baseURL, err := listenServe(baseSrv)
	if err != nil {
		return err
	}
	candURL, err := listenServe(candSrv)
	if err != nil {
		return err
	}
	defer func() {
		_ = baseSrv.Close()
		_ = candSrv.Close()
	}()

	reqPath := filepath.Join("examples", "requests.json")
	if _, err := os.Stat(reqPath); err != nil {
		tmp, err := os.CreateTemp("", "rpcdiff-requests-*.json")
		if err != nil {
			return err
		}
		reqPath = tmp.Name()
		defer os.Remove(reqPath)
		if _, err := tmp.Write(fixtures.DefaultRequests()); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stdout, "demo baseline  %s\n", baseURL)
	fmt.Fprintf(os.Stdout, "demo candidate %s\n", candURL)
	run, err := app.Run(context.Background(), app.Config{
		Baseline:  baseURL,
		Candidate: candURL,
		Requests:  reqPath,
		Output:    *output,
		HTML:      *htmlPath,
		Timeout:   *timeout,
		Workers:   2,
	})
	if err != nil {
		return err
	}
	report.PrintSummary(os.Stdout, run)
	fmt.Fprintf(os.Stdout, "wrote %s\n", *output)
	fmt.Fprintf(os.Stdout, "wrote %s\n", *htmlPath)
	return nil
}

func listenServe(srv *http.Server) (string, error) {
	ln, err := netListen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	srv.Addr = ln.Addr().String()
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}
