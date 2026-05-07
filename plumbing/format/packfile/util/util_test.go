package util_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	packutil "github.com/go-git/go-git/v6/plumbing/format/packfile/util"
)

func TestVariableLengthSizeOverflow(t *testing.T) {
	t.Parallel()

	// First byte: continuation set (0x80), low nibble does not matter.
	// Each subsequent 0x80 byte advances the shift by 7 without setting any
	// payload bit, until the shift would exceed 64-7 and the decoder must
	// reject the input.
	first := byte(0x90)
	tail := bytes.Repeat([]byte{0x80}, 9)

	_, err := packutil.VariableLengthSize(first, bytes.NewReader(tail))
	require.ErrorIs(t, err, packutil.ErrLengthOverflow)
}

func TestVariableLengthSizeBoundaryAccepts(t *testing.T) {
	t.Parallel()

	// Eight continuation bytes (shifts 4, 11, 18, 25, 32, 39, 46, 53) is
	// the maximum still inside the 64-bit type. The eighth byte's
	// continuation bit is clear, ending the loop without overflow.
	first := byte(0x80)
	tail := append(bytes.Repeat([]byte{0x80}, 7), 0x01)

	_, err := packutil.VariableLengthSize(first, bytes.NewReader(tail))
	require.NoError(t, err)
}

func TestDecodeLEB128Overflow(t *testing.T) {
	t.Parallel()

	// Eleven continuation bytes is enough to push shift past 64 bits on
	// either 32- or 64-bit platforms.
	input := append(bytes.Repeat([]byte{0x80}, 11), 0x01)

	_, _, err := packutil.DecodeLEB128(input)
	require.ErrorIs(t, err, packutil.ErrLengthOverflow)
}

func TestDecodeLEB128Roundtrip(t *testing.T) {
	t.Parallel()

	num, rest, err := packutil.DecodeLEB128([]byte{0x80, 0x01, 0xFF})
	require.NoError(t, err)
	assert.Equal(t, uint(128), num)
	assert.Equal(t, []byte{0xFF}, rest)
}

func TestDecodeLEB128FromReaderOverflow(t *testing.T) {
	t.Parallel()

	input := bytes.Repeat([]byte{0x80}, 11)

	_, err := packutil.DecodeLEB128FromReader(bytes.NewReader(input))
	require.ErrorIs(t, err, packutil.ErrLengthOverflow)
}
