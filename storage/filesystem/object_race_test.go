package filesystem

import (
	"sync"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
)

// TestConcurrentIndexAccess reproduces the race condition where s.index
// is accessed without proper mutex protection during concurrent operations.
//
// Without the mutex fixes in this PR:
//   - Race on s.index at object.go:210 (Reindex)
//   - Race on s.index at object.go:545 (EncodedObject checking s.index != nil)
//   - Race on s.index at object.go:193 (requireIndex)
//
// With the fixes, these s.index races are eliminated.
// Note: This test may still detect unrelated races in DotGit code.
//
// Run with: go test -race -run TestConcurrentIndexAccess
func TestConcurrentIndexAccess(t *testing.T) {
	t.Parallel()

	fixture := fixtures.ByTag("packfile").One()
	fs, err := fixture.DotGit()
	if err != nil {
		t.Fatal(err)
	}

	storage := NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = storage.Close() }()

	var wg sync.WaitGroup

	// Simulate concurrent operations that access s.index
	// This should trigger race detector if proper locking isn't in place
	for range 20 {
		wg.Add(3)

		// Reader 1: HashesWithPrefix (iterates over s.index)
		go func() {
			defer wg.Done()
			_, _ = storage.HashesWithPrefix([]byte{0x6e})
		}()

		// Reader 2: EncodedObject (checks s.index != nil)
		go func() {
			defer wg.Done()
			hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
			_, _ = storage.EncodedObject(plumbing.AnyObject, hash)
		}()

		// Writer: Reindex (sets s.index = nil)
		go func() {
			defer wg.Done()
			storage.Reindex()
		}()
	}

	wg.Wait()
}
