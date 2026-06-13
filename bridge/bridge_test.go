package bridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/mikefsq/lx200"
)

// fakeMount is a lx200.Mount that records target commits. SetTarget* widen the
// window between the RA and Dec writes (setDelay) so that, absent the OpLock, two
// concurrent goto sequences would interleave and commit a mismatched RA/Dec pair.
// It also satisfies lx200.OpLocker, modelling the shared serialization point a
// real *lx200.Conn provides.
type fakeMount struct {
	mu       sync.Mutex
	opMu     sync.Mutex
	curRA    float64
	curDec   float64
	pendRA   float64
	pendDec  float64
	slewing  bool
	halted   bool
	synced   bool
	setDelay time.Duration
	commits  [][2]float64
}

func (f *fakeMount) RA() (float64, error)  { f.mu.Lock(); defer f.mu.Unlock(); return f.curRA, nil }
func (f *fakeMount) Dec() (float64, error) { f.mu.Lock(); defer f.mu.Unlock(); return f.curDec, nil }

func (f *fakeMount) SetTargetRA(h float64) (bool, error) {
	f.mu.Lock()
	f.pendRA = h
	f.mu.Unlock()
	if f.setDelay > 0 {
		time.Sleep(f.setDelay)
	}
	return true, nil
}

func (f *fakeMount) SetTargetDec(d float64) (bool, error) {
	f.mu.Lock()
	f.pendDec = d
	f.mu.Unlock()
	if f.setDelay > 0 {
		time.Sleep(f.setDelay)
	}
	return true, nil
}

func (f *fakeMount) SlewToTarget() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.curRA, f.curDec = f.pendRA, f.pendDec
	f.commits = append(f.commits, [2]float64{f.pendRA, f.pendDec})
	return nil
}

func (f *fakeMount) SyncToTarget() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.curRA, f.curDec = f.pendRA, f.pendDec
	f.synced = true
	return "Matched", nil
}

func (f *fakeMount) Halt() error             { f.mu.Lock(); f.halted = true; f.mu.Unlock(); return nil }
func (f *fakeMount) Slewing() (bool, error)  { f.mu.Lock(); defer f.mu.Unlock(); return f.slewing, nil }
func (f *fakeMount) Tracking() (bool, error) { return true, nil }
func (f *fakeMount) SetTracking(bool) error  { return nil }

// OpLock makes fakeMount a lx200.OpLocker, the shared serialization point.
func (f *fakeMount) OpLock() func() { f.opMu.Lock(); return f.opMu.Unlock }

var _ lx200.Mount = (*fakeMount)(nil)
var _ lx200.OpLocker = (*fakeMount)(nil)

// startBridge spins up a Server on an ephemeral port and returns it plus a stop func.
func startBridge(t *testing.T, m MountFunc) *Server {
	t.Helper()
	s := New("127.0.0.1:0", m)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = s.Serve(ctx) }()
	// Wait for the listener to come up.
	for i := 0; i < 200; i++ {
		if s.Addr() != nil {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("bridge did not start listening")
	return nil
}

// client is a tiny LX200 test client.
type client struct {
	conn net.Conn
	r    *bufio.Reader
}

func dial(t *testing.T, s *Server) *client {
	t.Helper()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return &client{conn: c, r: bufio.NewReader(c)}
}

func (c *client) send(cmd string) { fmt.Fprintf(c.conn, ":%s#", cmd) }
func (c *client) sendRaw(b byte)  { c.conn.Write([]byte{b}) }
func (c *client) readByte(t *testing.T) byte {
	t.Helper()
	b, err := c.r.ReadByte()
	if err != nil {
		t.Fatalf("readByte: %v", err)
	}
	return b
}
func (c *client) readHash(t *testing.T) string {
	t.Helper()
	s, err := c.r.ReadString('#')
	if err != nil {
		t.Fatalf("readHash: %v", err)
	}
	return s
}

// readGoto reads a :MS# reply: a lone '0' on success, or '1'…'#' on failure.
func (c *client) readGoto(t *testing.T) string {
	t.Helper()
	b := c.readByte(t)
	if b == '0' {
		return "0"
	}
	return string(b) + c.readHash(t)
}

func TestReadsAndAck(t *testing.T) {
	f := &fakeMount{curRA: 1.5, curDec: 10}
	s := startBridge(t, func() (lx200.Mount, error) { return f, nil })
	c := dial(t, s)

	c.sendRaw(0x06)
	if got := c.readByte(t); got != 'P' {
		t.Errorf("ACK: got %q want 'P'", got)
	}
	c.send("GR")
	if got, want := c.readHash(t), lx200.FormatHMS(1.5)+"#"; got != want {
		t.Errorf("GR: got %q want %q", got, want)
	}
	c.send("GD")
	if got, want := c.readHash(t), lx200.FormatDMS(10, '*')+"#"; got != want {
		t.Errorf("GD: got %q want %q", got, want)
	}
	c.send("GVP")
	if got := c.readHash(t); got != "lx200-bridge#" {
		t.Errorf("GVP: got %q", got)
	}
}

func TestSetTargetValidation(t *testing.T) {
	f := &fakeMount{}
	s := startBridge(t, func() (lx200.Mount, error) { return f, nil })
	c := dial(t, s)

	c.send("Sr" + lx200.FormatHMS(5.0))
	if got := c.readByte(t); got != '1' {
		t.Errorf("valid Sr: got %q want '1'", got)
	}
	c.send("Sd" + lx200.FormatDMS(-95, '*')) // out of range
	if got := c.readByte(t); got != '0' {
		t.Errorf("out-of-range Sd: got %q want '0'", got)
	}
	c.send("Srgarbage")
	if got := c.readByte(t); got != '0' {
		t.Errorf("garbage Sr: got %q want '0'", got)
	}
}

func TestGotoCommitsTarget(t *testing.T) {
	f := &fakeMount{}
	s := startBridge(t, func() (lx200.Mount, error) { return f, nil })
	c := dial(t, s)

	c.send("Sr" + lx200.FormatHMS(3.25))
	c.readByte(t)
	c.send("Sd" + lx200.FormatDMS(45.5, '*'))
	c.readByte(t)
	c.send("MS")
	if got := c.readGoto(t); got != "0" {
		t.Fatalf("MS: got %q want '0'", got)
	}
	ra, _ := f.RA()
	dec, _ := f.Dec()
	if !approx(ra, 3.25) || !approx(dec, 45.5) {
		t.Errorf("committed target = (%v,%v) want (3.25,45.5)", ra, dec)
	}
}

func TestGotoWithoutTarget(t *testing.T) {
	f := &fakeMount{}
	s := startBridge(t, func() (lx200.Mount, error) { return f, nil })
	c := dial(t, s)
	c.send("MS")
	if got := c.readGoto(t); got == "0" {
		t.Errorf("MS without target should fail, got %q", got)
	}
}

func TestSync(t *testing.T) {
	f := &fakeMount{}
	s := startBridge(t, func() (lx200.Mount, error) { return f, nil })
	c := dial(t, s)
	c.send("Sr" + lx200.FormatHMS(12))
	c.readByte(t)
	c.send("Sd" + lx200.FormatDMS(0, '*'))
	c.readByte(t)
	c.send("CM")
	got := c.readHash(t)
	if got == "" || got[len(got)-1] != '#' {
		t.Errorf("CM reply %q not #-terminated", got)
	}
	if !f.synced {
		t.Error("mount was not synced")
	}
}

func TestHalt(t *testing.T) {
	f := &fakeMount{}
	s := startBridge(t, func() (lx200.Mount, error) { return f, nil })
	c := dial(t, s)
	c.send("Q")
	// :Q# has no reply; give the server a beat to process it.
	for i := 0; i < 100 && !f.isHalted(); i++ {
		time.Sleep(time.Millisecond)
	}
	if !f.isHalted() {
		t.Error("mount was not halted")
	}
}

func TestNotConnected(t *testing.T) {
	s := startBridge(t, func() (lx200.Mount, error) { return nil, errors.New("not connected") })
	c := dial(t, s)
	c.send("GR")
	if got := c.readHash(t); got != "#" {
		t.Errorf("GR while disconnected: got %q want '#'", got)
	}
	c.send("Sr" + lx200.FormatHMS(1))
	c.readByte(t)
	c.send("Sd" + lx200.FormatDMS(1, '*'))
	c.readByte(t)
	c.send("MS")
	if got := c.readGoto(t); got == "0" {
		t.Errorf("MS while disconnected should fail, got %q", got)
	}
}

// TestConcurrentNoInterleave drives the mount from the LX200 bridge and from a
// second front-end (simulating the Alpaca wrapper) at the same time, each aiming
// at a distinct RA/Dec pair, and asserts every committed target is one whole pair
// — never a mix. The shared OpLock is what makes this hold; with setDelay widening
// the set-target window, an unlocked sequence would commit a mismatched pair.
func TestConcurrentNoInterleave(t *testing.T) {
	f := &fakeMount{setDelay: time.Millisecond}
	s := startBridge(t, func() (lx200.Mount, error) { return f, nil })

	const iters = 40
	pairA := [2]float64{1.0, 10.0}
	pairB := [2]float64{2.0, 20.0}

	var wg sync.WaitGroup
	wg.Add(2)

	// Front-end 1: the LX200 bridge, over TCP.
	go func() {
		defer wg.Done()
		c := dial(t, s)
		for i := 0; i < iters; i++ {
			c.send("Sr" + lx200.FormatHMS(pairA[0]))
			c.readByte(t)
			c.send("Sd" + lx200.FormatDMS(pairA[1], '*'))
			c.readByte(t)
			c.send("MS")
			c.readGoto(t)
		}
	}()

	// Front-end 2: a direct consumer taking the same OpLock, like the Alpaca wrapper.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			func() {
				defer f.OpLock()()
				f.SetTargetRA(pairB[0])
				f.SetTargetDec(pairB[1])
				f.SlewToTarget()
			}()
		}
	}()

	wg.Wait()

	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.commits {
		okA := approx(c[0], pairA[0]) && approx(c[1], pairA[1])
		okB := approx(c[0], pairB[0]) && approx(c[1], pairB[1])
		if !okA && !okB {
			t.Fatalf("commit %d = (%v,%v): interleaved target — RA and Dec from different gotos", i, c[0], c[1])
		}
	}
}

func (f *fakeMount) isHalted() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.halted }

func approx(a, b float64) bool {
	d := a - b
	return d < 1e-6 && d > -1e-6
}
