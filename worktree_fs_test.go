package git

import (
	"os"
	"runtime"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		{"readme.md", false},
		{".gitignore", false},
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
		{"a", true},
		{"a\\b", true},
		{"a/b", true},
		{".gitm", true},
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
