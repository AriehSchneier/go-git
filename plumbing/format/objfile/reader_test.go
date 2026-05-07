package objfile

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

type SuiteReader struct {
	suite.Suite
}

func TestSuiteReader(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SuiteReader))
}

func (s *SuiteReader) TestReadObjfile() {
	tests := []struct {
		name         string
		objectFormat format.ObjectFormat
	}{
		{name: "sha1", objectFormat: format.SHA1},
		{name: "sha256", objectFormat: format.SHA256},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			for k, fixture := range objfileFixtures {
				com := fmt.Sprintf("%s test %d: ", tt.name, k)
				content, _ := base64.StdEncoding.DecodeString(fixture.content)
				data, _ := base64.StdEncoding.DecodeString(fixture.data)
				hash := readerFixtureHash(fixture, content, tt.objectFormat)

				testReader(s.T(), bytes.NewReader(data), hash, fixture.t, content, tt.objectFormat, com)
			}
		})
	}
}

func readerFixtureHash(
	fixture objfileFixture,
	content []byte,
	objectFormat format.ObjectFormat,
) plumbing.Hash {
	if objectFormat == format.SHA1 {
		return plumbing.NewHash(fixture.hash)
	}

	hasher := plumbing.NewHasher(objectFormat, fixture.t, int64(len(content)))
	_, _ = hasher.Write(content)
	return hasher.Sum()
}

func testReader(
	t *testing.T,
	source io.Reader,
	hash plumbing.Hash,
	o plumbing.ObjectType,
	content []byte,
	objectFormat format.ObjectFormat,
	_ string,
) {
	r, err := NewReader(source, objectFormat)
	assert.NoError(t, err)

	typ, size, err := r.Header()
	assert.NoError(t, err)
	assert.Equal(t, typ, o)
	assert.Len(t, content, int(size))

	rc, err := io.ReadAll(r)
	assert.NoError(t, err)
	assert.Equal(t, content, rc, fmt.Sprintf("content=%s, expected=%s", base64.StdEncoding.EncodeToString(rc), base64.StdEncoding.EncodeToString(content)))

	assert.Equal(t, hash, r.Hash()) // Test Hash() before close
	assert.NoError(t, r.Close())
}

func (s *SuiteReader) TestReadEmptyObjfile() {
	source := bytes.NewReader([]byte{})
	_, err := NewReader(source, format.SHA1)
	s.NotNil(err)
}

func (s *SuiteReader) TestReadGarbage() {
	source := bytes.NewReader([]byte("!@#$RO!@NROSADfinq@o#irn@oirfn"))
	_, err := NewReader(source, format.SHA1)
	s.NotNil(err)
}

func (s *SuiteReader) TestReadCorruptZLib() {
	data, _ := base64.StdEncoding.DecodeString("eAFLysaalPUjBgAAAJsAHw")
	source := bytes.NewReader(data)
	r, err := NewReader(source, format.SHA1)
	s.NoError(err)

	_, _, err = r.Header()
	s.NotNil(err)
}
