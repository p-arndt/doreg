package storage

// Usage is the size of the filesystem holding the data directory.
type Usage struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}
