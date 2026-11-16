//go:build unix

package storage

import "syscall"

// DiskUsage reports the filesystem holding path.
func DiskUsage(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	bs := uint64(st.Bsize)
	return Usage{Total: st.Blocks * bs, Free: st.Bavail * bs}, nil
}
