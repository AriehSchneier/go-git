package packfile

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
)

func TestDecodeLEB128Overflow(t *testing.T) {
	t.Parallel()

	input := append(bytes.Repeat([]byte{0x80}, 11), 0x01)

	_, _, err := packutil.DecodeLEB128(input)
	require.ErrorIs(t, err, packutil.ErrLengthOverflow)
}

func TestDecodeLEB128(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		want     uint
		wantRest []byte
	}{
		{
			name:     "single byte, small number",
			input:    []byte{0x01, 0xFF},
			want:     1,
			wantRest: []byte{0xFF},
		},
		{
			name:     "single byte, max value without continuation",
			input:    []byte{0x7F, 0xFF},
			want:     127,
			wantRest: []byte{0xFF},
		},
		{
			name:     "two bytes",
			input:    []byte{0x80, 0x01, 0xFF},
			want:     128,
			wantRest: []byte{0xFF},
		},
		{
			name:     "two bytes, larger number",
			input:    []byte{0xFF, 0x01, 0xFF},
			want:     255,
			wantRest: []byte{0xFF},
		},
		{
			name:     "three bytes",
			input:    []byte{0x80, 0x80, 0x01, 0xFF},
			want:     16384,
			wantRest: []byte{0xFF},
		},
		{
			name:     "empty remaining bytes",
			input:    []byte{0x01},
			want:     1,
			wantRest: []byte{},
		},
		{
			name:     "empty input",
			input:    []byte{},
			want:     0,
			wantRest: []byte{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotNum, gotRest, err := packutil.DecodeLEB128(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, gotNum, "decoded number mismatch")
			assert.Equal(t, tc.wantRest, gotRest, "remaining bytes mismatch")
		})
	}
}
