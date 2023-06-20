package git

import (
	"errors"
	"fmt"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/ports"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type BaseSuite struct {
	fixtures.Suite

	base   string
	port   int
	daemon *exec.Cmd
}

func (s *BaseSuite) SetUpTest(c *C) {
	if runtime.GOOS == "windows" {
		c.Skip(`git for windows has issues with write operations through git:// protocol.
		See https://github.com/git-for-windows/git/issues/907`)
	}

	var err error
	s.port, err = freePort()
	c.Assert(err, IsNil)

	s.base, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("go-git-protocol-%d", s.port))
	c.Assert(err, IsNil)
}

func (s *BaseSuite) StartDaemon(c *C) {
	s.daemon = exec.Command(
		"git",
		"daemon",
		fmt.Sprintf("--base-path=%s", s.base),
		"--export-all",
		"--enable=receive-pack",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", s.port),
		// Unless max-connections is limited to 1, a git-receive-pack
		// might not be seen by a subsequent operation.
		"--max-connections=1",
	)

	// Environment must be inherited in order to acknowledge GIT_EXEC_PATH if set.
	s.daemon.Env = os.Environ()

	err := s.daemon.Start()
	c.Assert(err, IsNil)

	// Connections might be refused if we start sending request too early.
	time.Sleep(time.Millisecond * 500)
}

func (s *BaseSuite) newEndpoint(c *C, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("git://localhost:%d/%s", s.port, name))
	c.Assert(err, IsNil)

	return ep
}

func (s *BaseSuite) prepareRepository(c *C, f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	c.Assert(err, IsNil)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	c.Assert(err, IsNil)

	return s.newEndpoint(c, name)
}

func (s *BaseSuite) TearDownTest(c *C) {
	_ = s.daemon.Process.Signal(os.Kill)
	_ = s.daemon.Wait()

	err := os.RemoveAll(s.base)
	c.Assert(err, IsNil)
}

var portManager = ports.NewPortManager()
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func freePort() (int, error) {
	portRes := ports.Reservation{
		Networks:     []tcpip.NetworkProtocolNumber{header.IPv4ProtocolNumber},
		Transport:    header.TCPProtocolNumber,
		Addr:         tcpip.AddrFrom4([4]byte{127, 0, 0, 1}),
		Port:         0,
		Flags:        ports.Flags{},
		BindToDevice: 0,
		Dest:         tcpip.FullAddress{},
	}

	gotPort, err := portManager.ReservePort(rng, portRes, nil)
	if err != nil {
		return 0, errors.New(err.String())
	}

	return int(gotPort), nil
}
