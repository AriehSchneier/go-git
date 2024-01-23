package git

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"errors"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/go-git/go-billy/v5"
)

var (
	// ErrEmptyCommit occurs when a commit is attempted using a clean
	// working tree, with no changes to be committed.
	ErrEmptyCommit = errors.New("cannot create empty commit: clean working tree")
)

// Commit stores the current contents of the index in a new commit along with
// a log message from the user describing the changes.
func (w *Worktree) Commit(msg string, opts *CommitOptions) (plumbing.Hash, error) {
	if err := opts.Validate(w.r); err != nil {
		return plumbing.ZeroHash, err
	}

	if opts.All {
		if err := w.autoAddModifiedAndDeleted(); err != nil {
			return plumbing.ZeroHash, err
		}
	}

	var treeHash plumbing.Hash

	if opts.Amend {
		head, err := w.r.Head()
		if err != nil {
			return plumbing.ZeroHash, err
		}

		t, err := w.r.getTreeFromCommitHash(head.Hash())
		if err != nil {
			return plumbing.ZeroHash, err
		}

		treeHash = t.Hash
		opts.Parents = []plumbing.Hash{head.Hash()}
	} else {
		idx, err := w.r.Storer.Index()
		if err != nil {
			return plumbing.ZeroHash, err
		}

		h := &buildTreeHelper{
			fs: w.Filesystem,
			s:  w.r.Storer,
		}

		treeHash, err = h.BuildTree(idx, opts)
		if err != nil {
			return plumbing.ZeroHash, err
		}
	}

	commit, err := w.buildCommitObject(msg, opts, treeHash)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return commit, w.updateHEAD(commit)
}

func (w *Worktree) autoAddModifiedAndDeleted() error {
	s, err := w.Status()
	if err != nil {
		return err
	}

	idx, err := w.r.Storer.Index()
	if err != nil {
		return err
	}

	for path, fs := range s {
		if fs.Worktree != Modified && fs.Worktree != Deleted {
			continue
		}

		if _, _, err := w.doAddFile(idx, s, path, nil); err != nil {
			return err
		}

	}

	return w.r.Storer.SetIndex(idx)
}

func (w *Worktree) updateHEAD(commit plumbing.Hash) error {
	head, err := w.r.Storer.Reference(plumbing.HEAD)
	if err != nil {
		return err
	}

	name := plumbing.HEAD
	if head.Type() != plumbing.HashReference {
		name = head.Target()
	}

	ref := plumbing.NewHashReference(name, commit)
	return w.r.Storer.SetReference(ref)
}

func (w *Worktree) buildCommitObject(msg string, opts *CommitOptions, tree plumbing.Hash) (plumbing.Hash, error) {
	commit := &object.Commit{
		Author:       *opts.Author,
		Committer:    *opts.Committer,
		Message:      msg,
		TreeHash:     tree,
		ParentHashes: opts.Parents,
	}

	// Convert SignKey into a Signer if set. Existing Signer should take priority.
	signer := opts.Signer
	if signer == nil && opts.SignKey != nil {
		signer = &gpgSigner{key: opts.SignKey}
	}
	if signer != nil {
		sig, err := w.buildCommitSignature(commit, signer)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		commit.PGPSignature = string(sig)
	}

	obj := w.r.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return w.r.Storer.SetEncodedObject(obj)
}

type gpgSigner struct {
	key *openpgp.Entity
}

func (s *gpgSigner) Public() crypto.PublicKey {
	return s.key.PrimaryKey
}

func (s *gpgSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	var cfg *packet.Config
	if opts != nil {
		cfg = &packet.Config{
			DefaultHash: opts.HashFunc(),
		}
	}

	var b bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&b, s.key, bytes.NewReader(digest), cfg); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (w *Worktree) buildCommitSignature(commit *object.Commit, signer crypto.Signer) ([]byte, error) {
	encoded := &plumbing.MemoryObject{}
	if err := commit.Encode(encoded); err != nil {
		return nil, err
	}
	r, err := encoded.Reader()
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return signer.Sign(rand.Reader, b, nil)
}

// buildTreeHelper converts a given index.Index file into multiple git objects
// reading the blobs from the given filesystem and creating the trees from the
// index structure. The created objects are pushed to a given Storer.
type buildTreeHelper struct {
	fs billy.Filesystem
	s  storage.Storer

	trees   map[string]*object.Tree
	entries map[string]*object.TreeEntry
}

// BuildTree builds the tree objects and push its to the storer, the hash
// of the root tree is returned.
func (h *buildTreeHelper) BuildTree(idx *index.Index, opts *CommitOptions) (plumbing.Hash, error) {
	if len(idx.Entries) == 0 && (opts == nil || !opts.AllowEmptyCommits) {
		return plumbing.ZeroHash, ErrEmptyCommit
	}

	const rootNode = ""
	h.trees = map[string]*object.Tree{rootNode: {}}
	h.entries = map[string]*object.TreeEntry{}

	for _, e := range idx.Entries {
		if err := h.commitIndexEntry(e); err != nil {
			return plumbing.ZeroHash, err
		}
	}

	return h.copyTreeToStorageRecursive(rootNode, h.trees[rootNode])
}

func (h *buildTreeHelper) commitIndexEntry(e *index.Entry) error {
	parts := strings.Split(e.Name, "/")

	var fullpath string
	for _, part := range parts {
		parent := fullpath
		fullpath = path.Join(fullpath, part)

		h.doBuildTree(e, parent, fullpath)
	}

	return nil
}

func (h *buildTreeHelper) doBuildTree(e *index.Entry, parent, fullpath string) {
	if _, ok := h.trees[fullpath]; ok {
		return
	}

	if _, ok := h.entries[fullpath]; ok {
		return
	}

	te := object.TreeEntry{Name: path.Base(fullpath)}

	if fullpath == e.Name {
		te.Mode = e.Mode
		te.Hash = e.Hash
	} else {
		te.Mode = filemode.Dir
		h.trees[fullpath] = &object.Tree{}
	}

	h.trees[parent].Entries = append(h.trees[parent].Entries, te)
}

type sortableEntries []object.TreeEntry

func (sortableEntries) sortName(te object.TreeEntry) string {
	if te.Mode == filemode.Dir {
		return te.Name + "/"
	}
	return te.Name
}
func (se sortableEntries) Len() int               { return len(se) }
func (se sortableEntries) Less(i int, j int) bool { return se.sortName(se[i]) < se.sortName(se[j]) }
func (se sortableEntries) Swap(i int, j int)      { se[i], se[j] = se[j], se[i] }

func (h *buildTreeHelper) copyTreeToStorageRecursive(parent string, t *object.Tree) (plumbing.Hash, error) {
	sort.Sort(sortableEntries(t.Entries))
	for i, e := range t.Entries {
		if e.Mode != filemode.Dir && !e.Hash.IsZero() {
			continue
		}

		path := path.Join(parent, e.Name)

		var err error
		e.Hash, err = h.copyTreeToStorageRecursive(path, h.trees[path])
		if err != nil {
			return plumbing.ZeroHash, err
		}

		t.Entries[i] = e
	}

	o := h.s.NewEncodedObject()
	if err := t.Encode(o); err != nil {
		return plumbing.ZeroHash, err
	}

	hash := o.Hash()
	if h.s.HasEncodedObject(hash) == nil {
		return hash, nil
	}
	return h.s.SetEncodedObject(o)
}
