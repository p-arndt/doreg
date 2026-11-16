//go:build !unix

package storage

import "errors"

// DiskUsage is not implemented on this platform.
func DiskUsage(string) (Usage, error) { return Usage{}, errors.ErrUnsupported }
