package packfile

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
)

func FuzzScanner(f *testing.F) {
	// Seed from a real packfile when available.
	if pf, err := fixtures.Basic().One().Packfile(); err == nil {
		if data, err := io.ReadAll(pf); err == nil {
			f.Add(data)
		}
	}

	// Minimal valid pack: PACK + version(2) + object count(0) + SHA1 trailer.
	var minimal bytes.Buffer
	minimal.WriteString("PACK")
	_ = binary.Write(&minimal, binary.BigEndian, uint32(2))
	_ = binary.Write(&minimal, binary.BigEndian, uint32(0))
	sum := sha1.Sum(minimal.Bytes())
	minimal.Write(sum[:])
	f.Add(minimal.Bytes())

	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		s := NewScanner(bytes.NewReader(data))
		for s.Scan() {
			d := s.Data()
			_ = d.Section
			_ = d.Value()
		}
		_ = s.Error()
	})
}
