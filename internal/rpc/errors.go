package rpc

import "fmt"

var errEmptyRequestFile = fmt.Errorf("request file contains no requests")

func fmtRequest(i int, err error) error {
	return fmt.Errorf("request[%d]: %w", i, err)
}
