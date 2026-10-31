package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// helperFiles serves esa-helper binaries from a directory, cached in memory.
type helperFiles struct {
	dir   string
	mu    sync.Mutex
	cache map[string][]byte
}

func newHelperFiles(dir string) *helperFiles {
	return &helperFiles{dir: dir, cache: map[string][]byte{}}
}

// Get returns the helper for a GOARCH.
func (h *helperFiles) Get(arch string) ([]byte, error) {
	if arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("no helper for architecture %q", arch)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if b, ok := h.cache[arch]; ok {
		return b, nil
	}
	path := filepath.Join(h.dir, "esa-helper-linux-"+arch)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("helper binary %s is missing; build it with `pnpm --filter @esa/esa build:helpers`", path)
	}
	h.cache[arch] = b
	return b, nil
}
