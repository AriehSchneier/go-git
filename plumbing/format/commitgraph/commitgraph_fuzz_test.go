package commitgraph

import (
	"bytes"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
)

func FuzzOpenFileIndex(f *testing.F) {
	// Seed from real commit-graph fixture when available.
	for _, fix := range fixtures.ByTag("commit-graph") {
		if dotgit, err := fix.DotGit(); err == nil {
			path := dotgit.Join("objects", "info", "commit-graph")
			if fh, err := dotgit.Open(path); err == nil {
				if data, err := io.ReadAll(fh); err == nil {
					f.Add(data)
				}
				_ = fh.Close()
			}
		}
	}

	// Minimal header: 4-byte signature + version(1) + hash-version(1) +
	// number-of-chunks(1) + number-of-base-graphs(1).
	f.Add([]byte("CGPH\x01\x01\x00\x00"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		idx, err := OpenFileIndex(struct {
			io.ReaderAt
			io.Closer
		}{
			bytes.NewReader(data),
			io.NopCloser(nil),
		})
		if err != nil {
			return
		}
		defer idx.Close()

		// Walk the index to exercise fanout-driven offset math in
		// GetCommitDataByIndex and the OID lookup / generation-data paths.
		hashes := idx.Hashes()
		const maxIters = 4096
		n := min(len(hashes), maxIters)
		for i := range n {
			_, _ = idx.GetIndexByHash(hashes[i])
			_, _ = idx.GetHashByIndex(uint32(i))
			_, _ = idx.GetCommitDataByIndex(uint32(i))
		}
	})
}
