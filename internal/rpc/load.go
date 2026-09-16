package rpc

import (
	"encoding/json"
	"os"
)

func LoadRequests(path string) ([]Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reqs []Request
	if err := json.Unmarshal(data, &reqs); err != nil {
		return nil, err
	}
	if len(reqs) == 0 {
		return nil, errEmptyRequestFile
	}
	for i, r := range reqs {
		if err := r.Validate(); err != nil {
			return nil, fmtRequest(i, err)
		}
		if len(r.JSONRPC) == 0 {
			reqs[i].JSONRPC = "2.0"
		}
		if len(r.Params) == 0 {
			reqs[i].Params = json.RawMessage("[]")
		}
	}
	return reqs, nil
}
