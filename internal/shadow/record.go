package shadow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"rpcdiff/internal/compare"
)

// resultStore writes one result per JSONL record while traffic is flowing.
// The report is materialized only after the proxy stops, so request volume
// does not grow an in-memory slice during a long observation window.
type resultStore struct {
	mu       sync.Mutex
	file     *os.File
	writeErr error
}

func newResultStore() (*resultStore, error) {
	file, err := os.CreateTemp("", "rpcdiff-shadow-*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("create shadow result spool: %w", err)
	}
	return &resultStore{file: file}, nil
}

func (s *resultStore) append(result compare.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	data, err := json.Marshal(result)
	if err != nil {
		s.writeErr = err
		return err
	}
	data = append(data, '\n')
	_, err = s.file.Write(data)
	if err != nil {
		s.writeErr = err
	}
	return err
}

func (s *resultStore) readAll() ([]compare.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.file.Sync(); err != nil {
		_ = s.file.Close()
		_ = os.Remove(s.file.Name())
		return nil, err
	}
	writeErr := s.writeErr
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		_ = s.file.Close()
		_ = os.Remove(s.file.Name())
		return nil, err
	}
	var results []compare.Result
	dec := json.NewDecoder(s.file)
	for {
		var result compare.Result
		err := dec.Decode(&result)
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = s.file.Close()
			_ = os.Remove(s.file.Name())
			return nil, err
		}
		results = append(results, result)
	}
	name := s.file.Name()
	closeErr := s.file.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return nil, closeErr
	}
	if removeErr != nil {
		return nil, removeErr
	}
	if writeErr != nil {
		return results, writeErr
	}
	return results, nil
}
