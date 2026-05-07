package packfile

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"io"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v6"
)

func FuzzParser(f *testing.F) {
	if pf, err := fixtures.Basic().One().Packfile(); err == nil {
		if data, rerr := io.ReadAll(pf); rerr == nil {
			f.Add(data)
		}
	}

	var overflow bytes.Buffer
	overflow.WriteString("PACK")
	_ = binary.Write(&overflow, binary.BigEndian, uint32(2))
	_ = binary.Write(&overflow, binary.BigEndian, uint32(1))
	overflow.WriteByte(0x90)
	overflow.Write(bytes.Repeat([]byte{0x80}, 9))
	sum := sha1.Sum(overflow.Bytes())
	overflow.Write(sum[:])
	f.Add(overflow.Bytes())

	f.Fuzz(func(_ *testing.T, data []byte) {
		p := NewParser(bytes.NewReader(data))
		_, _ = p.Parse()
	})
}
