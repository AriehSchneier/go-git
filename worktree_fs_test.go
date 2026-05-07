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

func TestValidPathProtectNTFS(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), true, false)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{".git . . .", true},
		{".git . . ", true},
		{".git ", true},
		{".git.", true},
		{".git::$INDEX_ALLOCATION", true},
		{"CON", true},
		{"aux.txt", true},
		{"sub/NUL", true},
		{"sub/COM1.txt", true},
		{"CONIN$", true},
		{"readme.md", false},
		{".gitignore", false},
		{"CONNECT", false},
	}

	if runtime.GOOS == "windows" {
		// filepath.VolumeName only parses volume names on Windows.
		tests = append(tests, []struct {
			path    string
			wantErr bool
		}{
			{"\\\\a\\b", true},
			{"C:\\a\\b", true},
		}...)
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

func TestValidPathProtectNTFSDisabled(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	paths := []string{
		".git . . .",
		".git ",
		".git.",
		".git::$INDEX_ALLOCATION",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(p)
			assert.NoError(t, err, "NTFS checks should not apply when protectNTFS is false")
		})
	}
}

func TestWindowsValidPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{".git", false},
		{".git . . .", false},
		{".git ", false},
		{".git  ", false},
		{".git . .", false},
		{".git . .", false},
		{".git::$INDEX_ALLOCATION", false},
		{".git:", false},
		{"CON", false},
		{"con", false},
		{"CON.txt", false},
		{"CON:ads", false},
		{"CON ", false},
		{"PRN", false},
		{"AUX", false},
		{"NUL", false},
		{"COM1", false},
		{"COM9", false},
		{"LPT1", false},
		{"LPT9", false},
		{"CONIN$", false},
		{"CONOUT$", false},
		{"a", true},
		{"a\\b", true},
		{"a/b", true},
		{".gitm", true},
		{"CONNECT", true},
		{"comic", true},
		{"COM", true},
		{"COM0", true},
		{"LPT0", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			got := windowsValidPath(tc.path)
			assert.Equal(t, tc.want, got)
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

func TestWorktreeFilesystemRejectsNTFSPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), true, false)

	ntfsPaths := []string{
		".git /config",
		".git./config",
		".git::$INDEX_ALLOCATION/config",
	}

	for _, p := range ntfsPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()

			_, err := fs.Create(p)
			assert.Error(t, err, "Create should reject NTFS path %q", p)
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

func TestIsHFSDotGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		part string
		want bool
	}{
		{".git", true},
		{".Git", true},
		{".GIT", true},
		{".gIt", true},
		{".g\u200cit", true},
		{".gi\u200dt", true},
		{".gi\ufefft", true},
		{"\u200e.git", true},
		{".g\u200ci\u200dt", true},
		{".gitmodules", false},
		{".gitignore", false},
		{".git2", false},
		{"git", false},
		{".gxt", false},
		{"", false},
		{".", false},
		{".g\x80it", false},
	}

	for _, tc := range tests {
		t.Run(tc.part, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isHFSDotGit(tc.part))
		})
	}
}

func TestValidPathProtectHFS(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, true)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{".git", true},
		{".g\u200cit", true},
		{"\u200e.git", true},
		{".Git", true},
		{".GIT", true},
		{".gitignore", false},
		{"readme.md", false},
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

func TestValidPathProtectHFSDisabled(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, false)

	hfsPaths := []string{
		".g\u200cit",
		"\u200e.git",
		".gi\ufefft",
	}

	for _, p := range hfsPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			err := fs.validPath(p)
			assert.NoError(t, err, "HFS checks should not apply when protectHFS is false")
		})
	}
}

func TestWorktreeFilesystemRejectsHFSPaths(t *testing.T) {
	t.Parallel()

	fs := newWorktreeFilesystem(memfs.New(), false, true)

	hfsPaths := []string{
		".g\u200cit/config",
		"\u200e.git/config",
	}

	for _, p := range hfsPaths {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, err := fs.Create(p)
			assert.Error(t, err, "Create should reject HFS path %q", p)
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
