package git

import (
	"runtime"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/assert"
)

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
