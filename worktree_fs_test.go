package git

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestValidPath(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{".git", true},
		{".git/b", true},
		{".git\\b", true},
		{"git~1", true},
		{"a/../b", true},
		{"a\\..\\b", true},
		{"/", true},
		{"", true},
		{".gitmodules", false},
		{".gitignore", false},
		{"a..b", false},
		{".", true},
		{"a/.git/b", true},
		{"a\\.git\\b", true},
		{"a/.git", false},
		{"a\\.git", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(tc.path)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWorktreeFilesystemRejectsInvalidPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	badPaths := []string{
		".git/config",
		".git/objects/pack/file",
		"git~1/HEAD",
		"../escape",
		"a/../../etc/passwd",
	}

	for _, p := range badPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			_, err := fs.Create(p)
			assert.Error(t, err, "Create should reject %q", p)

			_, err = fs.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			assert.Error(t, err, "OpenFile should reject %q", p)

			err = fs.Remove(p)
			assert.Error(t, err, "Remove should reject %q", p)

			err = fs.MkdirAll(p, 0o755)
			assert.Error(t, err, "MkdirAll should reject %q", p)

			err = fs.Symlink("target", p)
			assert.Error(t, err, "Symlink should reject %q", p)
		})
	}

	for _, p := range badPaths {
		t.Run("Rename/from/"+p, func(t *testing.T) {
			t.Parallel()
			err := fs.Rename(p, "dst")
			assert.Error(t, err, "Rename should reject from=%q", p)
		})
		t.Run("Rename/to/"+p, func(t *testing.T) {
			t.Parallel()
			err := fs.Rename("src", p)
			assert.Error(t, err, "Rename should reject to=%q", p)
		})
	}
}

func TestWorktreeFilesystemAllowsValidPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	validPaths := []string{
		"readme.md",
		"src/main.go",
		".gitignore",
	}

	for _, p := range validPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			f, err := fs.Create(p)
			require.NoError(t, err, "Create should allow %q", p)
			require.NoError(t, f.Close())

			err = fs.Remove(p)
			assert.NoError(t, err, "Remove should allow %q", p)
		})
	}
}

func TestWorktreeFilesystemReturnsWorktreeFilesystem(t *testing.T) {
	t.Parallel()

	t.Run("via Repository.Worktree", func(t *testing.T) {
		t.Parallel()

		mfs := memfs.New()
		r, err := Init(memory.NewStorage(), WithWorkTree(mfs))
		require.NoError(t, err)

		w, err := r.Worktree()
		require.NoError(t, err)

		assert.Equal(t, mfs, w.Filesystem())

		_, err = w.filesystem.Create(".git/file")
		assert.Error(t, err, "Create through worktreeFilesystem should reject .git paths")
	})

	t.Run("via struct literal", func(t *testing.T) {
		t.Parallel()

		mfs := memfs.New()
		w := &Worktree{filesystem: newWorktreeFilesystem(mfs, false, false)}

		assert.Equal(t, mfs, w.Filesystem())

		_, err := w.filesystem.Create(".git/file")
		assert.Error(t, err)
	})
}

// assertOpsRejected exercises the read/write surface of the wrapper
// against a dangerous path and asserts every operation is rejected. Used
// across the symlink tests to demonstrate that the wrapper's protections
// hold no matter how the call site got there.
func assertOpsRejected(t *testing.T, fs *worktreeFilesystem, p string) {
	t.Helper()

	_, err := fs.Open(p)
	assert.ErrorContains(t, err, "open:", "Open should reject %q", p)

	_, err = fs.Create(p)
	assert.ErrorContains(t, err, "create:", "Create should reject %q", p)

	_, err = fs.OpenFile(p, os.O_RDWR, 0o644)
	assert.ErrorContains(t, err, "openfile:", "OpenFile should reject %q", p)

	err = fs.Remove(p)
	assert.ErrorContains(t, err, "remove:", "Remove should reject %q", p)

	_, err = fs.Lstat(p)
	assert.ErrorContains(t, err, "lstat:", "Lstat should reject %q", p)

	_, err = fs.Readlink(p)
	assert.ErrorContains(t, err, "readlink:", "Readlink should reject %q", p)
}

func TestWorktreeFilesystemSymlinkRejectsDangerousPaths(t *testing.T) {
	t.Parallel()

	badPaths := []string{
		".git",
		".git/config",
		".git/hooks/pre-commit",
		"git~1/HEAD",
		"../escape",
		"a/../../etc/passwd",
	}

	for _, p := range badPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			fs := newWorktreeFilesystem(memfs.New(), false, false)

			err := fs.Symlink("safe-target.txt", p)
			assert.ErrorContains(t, err, "symlink:", "Symlink should reject link name %q", p)

			err = fs.Symlink(p, "safe-link")
			assert.ErrorContains(t, err, "symlink:", "Symlink should reject target %q", p)

			assertOpsRejected(t, fs, p)
		})
	}
}

func TestWorktreeFilesystemSymlinkAllowsValidLink(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	require.NoError(t, fs.Symlink("target.txt", "link"))

	got, err := fs.Readlink("link")
	require.NoError(t, err)
	assert.Equal(t, "target.txt", got)

	assertOpsRejected(t, fs, ".git/config")
}

func TestWorktreeFilesystemReadlinkValidatesPath(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)
	require.NoError(t, fs.Symlink("target.txt", "good-link"))

	t.Run("rejects bad link path", func(t *testing.T) {
		t.Parallel()
		_, err := fs.Readlink(".git/config")
		assert.ErrorContains(t, err, "readlink:")
	})

	t.Run("allows valid link path", func(t *testing.T) {
		t.Parallel()
		got, err := fs.Readlink("good-link")
		require.NoError(t, err)
		assert.Equal(t, "target.txt", got)
	})

	assertOpsRejected(t, fs, ".git/config")
}

// TestWorktreeFilesystemFollowsSymlinkOnOpen verifies that Open on a
// symlink-named path follows the link via the underlying billy.Filesystem
// for legitimate links, while still rejecting any operation that targets a
// dangerous path directly.
func TestWorktreeFilesystemFollowsSymlinkOnOpen(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	require.NoError(t, util.WriteFile(fs, "data.txt", []byte("hello"), 0o644))
	require.NoError(t, fs.Symlink("data.txt", "alias"))

	f, err := fs.Open("alias")
	require.NoError(t, err)
	defer f.Close()

	buf := make([]byte, 5)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))

	assertOpsRejected(t, fs, ".git/config")
}

// TestWorktreeFilesystemRejectsOpsOnPreExistingDotGitSymlink covers the case
// where a `.git` symlink was placed on the underlying filesystem before the
// wrapper saw it (e.g. a crafted on-disk repository). The wrapper validates
// the path the caller passed, so every operation against the `.git` name is
// refused regardless of what the symlink resolves to.
func TestWorktreeFilesystemRejectsOpsOnPreExistingDotGitSymlink(t *testing.T) {
	t.Parallel()

	mfs := memfs.New()

	require.NoError(t, util.WriteFile(mfs, "real.txt", []byte("data"), 0o644))
	require.NoError(t, mfs.Symlink("real.txt", ".git"))

	fs := newWorktreeFilesystem(mfs, false, false)

	assertOpsRejected(t, fs, ".git")
	assertOpsRejected(t, fs, ".git/config")
}

// assertOpsAllowed verifies the round-trip read/write surface for a path
// the wrapper should accept: write a payload, read it back, and Lstat it.
func assertOpsAllowed(t *testing.T, fs *worktreeFilesystem, p string) {
	t.Helper()

	const payload = "payload"
	require.NoError(t, util.WriteFile(fs, p, []byte(payload), 0o644))

	f, err := fs.Open(p)
	require.NoError(t, err, "Open should accept %q", p)
	t.Cleanup(func() { _ = f.Close() })

	buf := make([]byte, len(payload))
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, payload, string(buf[:n]))

	fi, err := fs.Lstat(p)
	require.NoError(t, err, "Lstat should accept %q", p)
	assert.Equal(t, int64(len(payload)), fi.Size())
}

func TestWorktreeFilesystemAbsolutePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		wantReject bool
	}{
		{"reject /.git", "/.git", true},
		{"reject /.git/config", "/.git/config", true},
		{"reject /.git/objects/pack/file", "/.git/objects/pack/file", true},
		{"reject /git~1/HEAD", "/git~1/HEAD", true},
		{"reject /sub/.git/config", "/sub/.git/config", true},
		{"allow /readme.md", "/readme.md", false},
		{"allow /src/main.go", "/src/main.go", false},
		{"allow /.gitignore", "/.gitignore", false},
		{"allow /submodule/.git", "/submodule/.git", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := newWorktreeFilesystem(memfs.New(), false, false)

			if tc.wantReject {
				assertOpsRejected(t, fs, tc.path)

				err := fs.Symlink("safe-target.txt", tc.path)
				assert.ErrorContains(t, err, "symlink:", "Symlink should reject link %q", tc.path)

				err = fs.Symlink(tc.path, "safe-link")
				assert.ErrorContains(t, err, "symlink:", "Symlink should reject target %q", tc.path)
				return
			}

			assertOpsAllowed(t, fs, tc.path)
		})
	}
}

// TestCherryPickPathValidationMatchesGit verifies that go-git and upstream
// Git both reject cherry-picking commits that contain dangerous paths.
//
// For each test case, a commit is crafted (via go-git plumbing) in an
// on-disk repository with a tree containing a single bad path. Both go-git
// CherryPick and `git cherry-pick` are run against it. Both must reject.
func TestCherryPickPathValidationMatchesGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping path validation conformance test in short mode")
	}
	t.Parallel()

	tests := []struct {
		name string
		// path is the file path to place in the crafted commit's tree.
		// Nested paths (containing /) are built as nested tree objects.
		path string
		// config overrides to set before running cherry-pick.
		config map[string]string
		// skipGit skips the upstream git cherry-pick check. Used for
		// checks that go-git enforces but upstream git does not on this
		// platform (e.g. reserved device names are only checked by
		// compat/mingw.c, which is not compiled on non-Windows).
		skipGit bool
	}{
		{
			name: ".git at root",
			path: ".git/config",
		},
		{
			name: ".git in subdirectory",
			path: "subdir/.git/config",
		},
		{
			name: "git~1 8.3 short name",
			path: "git~1/config",
		},
		{
			name: "dot-dot traversal",
			path: "a/../../etc/passwd",
		},
		{
			name: "single dot component",
			path: "a/./b",
		},
		{
			name:   "NTFS trailing space on .git",
			path:   ".git /config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "NTFS trailing dot on .git",
			path:   ".git./config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:   "NTFS alternate data stream",
			path:   ".git::$INDEX_ALLOCATION/config",
			config: map[string]string{"core.protectNTFS": "true"},
		},
		{
			name:    "NTFS reserved device name CON",
			path:    "CON/file",
			config:  map[string]string{"core.protectNTFS": "true"},
			skipGit: runtime.GOOS != "windows",
		},
		{
			name:    "NTFS reserved device name NUL",
			path:    "NUL",
			config:  map[string]string{"core.protectNTFS": "true"},
			skipGit: runtime.GOOS != "windows",
		},
		{
			name:   "HFS+ zero-width character in .git",
			path:   ".g\u200cit/config",
			config: map[string]string{"core.protectHFS": "true"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			r, err := PlainInit(dir, false)
			require.NoError(t, err)

			w, err := r.Worktree()
			require.NoError(t, err)

			require.NoError(t, util.WriteFile(w.Filesystem(), "README", []byte("init"), 0o644))
			_, err = w.Add("README")
			require.NoError(t, err)

			initHash, err := w.Commit("initial commit\n", &CommitOptions{Author: defaultSignature()})
			require.NoError(t, err)

			for k, v := range tc.config {
				gitConfig(t, dir, k, v)
			}

			initCommit, err := r.CommitObject(initHash)
			require.NoError(t, err)

			badCommit := buildBadCommit(t, r.Storer, initCommit, initHash, tc.path)

			// Re-open so config overrides take effect in the worktreeFilesystem.
			r, err = PlainOpen(dir)
			require.NoError(t, err)

			w, err = r.Worktree()
			require.NoError(t, err)

			goGitErr := w.CherryPick(
				&CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true},
				TheirsMergeStrategy, badCommit,
			)
			assert.Error(t, goGitErr, "go-git should reject cherry-pick of %q", tc.path)

			if !tc.skipGit {
				require.NoError(t, w.Reset(&ResetOptions{Commit: initHash, Mode: HardReset}))

				gitErr := gitCherryPick(t, dir, badCommit.Hash.String())
				assert.Error(t, gitErr, "git should reject cherry-pick of %q", tc.path)
			}
		})
	}
}

func buildBadCommit(t *testing.T, s storer.Storer, parent *object.Commit, parentHash plumbing.Hash, filePath string) *object.Commit {
	t.Helper()

	content := []byte("exploit")
	blobObj := s.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	blobObj.SetSize(int64(len(content)))
	bw, err := blobObj.Writer()
	require.NoError(t, err)
	_, err = bw.Write(content)
	require.NoError(t, err)
	require.NoError(t, bw.Close())
	blobHash, err := s.SetEncodedObject(blobObj)
	require.NoError(t, err)

	// Build nested tree structure from leaf to root.
	parts := strings.Split(filePath, "/")
	leafHash := blobHash
	leafMode := filemode.Regular

	for i := len(parts) - 1; i >= 1; i-- {
		tree := &object.Tree{
			Entries: []object.TreeEntry{
				{Name: parts[i], Mode: leafMode, Hash: leafHash},
			},
		}
		treeObj := s.NewEncodedObject()
		require.NoError(t, tree.Encode(treeObj))
		leafHash, err = s.SetEncodedObject(treeObj)
		require.NoError(t, err)
		leafMode = filemode.Dir
	}

	parentTree, err := parent.Tree()
	require.NoError(t, err)

	entries := make([]object.TreeEntry, len(parentTree.Entries), len(parentTree.Entries)+1)
	copy(entries, parentTree.Entries)
	entries = append(entries, object.TreeEntry{
		Name: parts[0],
		Mode: leafMode,
		Hash: leafHash,
	})
	rootTree := &object.Tree{Entries: entries}
	sort.Sort(object.TreeEntrySorter(rootTree.Entries))
	rootObj := s.NewEncodedObject()
	require.NoError(t, rootTree.Encode(rootObj))
	rootHash, err := s.SetEncodedObject(rootObj)
	require.NoError(t, err)

	commit := &object.Commit{
		Author:       *defaultSignature(),
		Committer:    *defaultSignature(),
		Message:      "bad path: " + filePath + "\n",
		TreeHash:     rootHash,
		ParentHashes: []plumbing.Hash{parentHash},
	}
	commitObj := s.NewEncodedObject()
	require.NoError(t, commit.Encode(commitObj))
	commitHash, err := s.SetEncodedObject(commitObj)
	require.NoError(t, err)

	result, err := object.GetCommit(s, commitHash)
	require.NoError(t, err)
	return result
}

func gitConfig(t *testing.T, dir, key, value string) {
	t.Helper()
	cmd := exec.Command("git", "config", key, value)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git config %s %s: %s", key, value, out)
}

func gitCherryPick(t *testing.T, dir, hash string) error {
	t.Helper()
	cmd := exec.Command("git", "cherry-pick", hash)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		abort := exec.Command("git", "cherry-pick", "--abort")
		abort.Dir = dir
		_ = abort.Run()
		return fmt.Errorf("git cherry-pick %s: %s: %w", hash, out, err)
	}
	return nil
}
