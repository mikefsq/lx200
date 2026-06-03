package lx200

import (
	"errors"
	"io"
	"math"
	"net"
	"sync"
	"testing"
	"time"
)

// TestNetPipeRoundTrip exercises the real net.Conn path (including the
// SetReadDeadline branch) end to end against a goroutine acting as the mount.
func TestNetPipeRoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := New(client, time.Second)

	go func() {
		buf := make([]byte, 32)
		if _, err := server.Read(buf); err != nil { // consume ":GR#"
			return
		}
		server.Write([]byte("12:00:00#"))
	}()

	ra, err := c.RA()
	if err != nil {
		t.Fatalf("RA over net.Pipe: %v", err)
	}
	if math.Abs(ra-12) > 1e-6 {
		t.Errorf("RA = %v, want 12", ra)
	}
}

// TestNetPipeTimeout verifies the net.Conn deadline produces ErrTimeout when the
// mount never replies (the deadliner branch).
func TestNetPipeTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := New(client, 200*time.Millisecond)

	go func() {
		buf := make([]byte, 32)
		server.Read(buf) // consume command, never reply
	}()

	start := time.Now()
	if _, err := c.RA(); err != ErrTimeout {
		t.Errorf("RA err = %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d < 150*time.Millisecond || d > 2*time.Second {
		t.Errorf("timeout took %v, want ~200ms", d)
	}
}

// TestDialTCP exercises DialTCP + Close against a real loopback TCP listener.
func TestDialTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 32)
		conn.Read(buf) // ":GR#"
		conn.Write([]byte("12:00:00#"))
	}()

	tr, err := DialTCP(ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	c := New(tr, time.Second)
	if ra, err := c.RA(); err != nil || math.Abs(ra-12) > 1e-6 {
		t.Errorf("RA over TCP = %v, %v", ra, err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestDialTCPError(t *testing.T) {
	// Port 1 is privileged/closed — connection refused.
	if _, err := DialTCP("127.0.0.1:1", 300*time.Millisecond); err == nil {
		t.Errorf("DialTCP to closed port: want error")
	}
}

// TestAwait reads an unsolicited '#'-terminated push (no command sent) and
// times out cleanly when nothing arrives.
func TestAwait(t *testing.T) {
	f := &fakeMount{replies: map[string]string{}}
	f.rbuf = []byte(":MM0#") // mount pushed a slew-completion token
	c := New(f, 200*time.Millisecond)
	if tok, err := c.Await(150 * time.Millisecond); err != nil || tok != ":MM0" {
		t.Errorf("Await = %q, %v; want :MM0", tok, err)
	}
	if _, err := c.Await(80 * time.Millisecond); err != ErrTimeout {
		t.Errorf("Await with no push = %v, want ErrTimeout", err)
	}
}

// latentMount returns "no data yet" (0, nil) several times before dribbling the
// reply — simulating serial latency, to exercise the readUntil wait loop.
type latentMount struct {
	reply       []byte
	skips, done int
	pos         int
}

func (m *latentMount) Write(p []byte) (int, error) { return len(p), nil }
func (m *latentMount) Read(p []byte) (int, error) {
	if m.done < m.skips {
		m.done++
		return 0, nil
	}
	if m.pos >= len(m.reply) {
		return 0, nil
	}
	p[0] = m.reply[m.pos]
	m.pos++
	return 1, nil
}
func (m *latentMount) Close() error { return nil }

func TestLatentReply(t *testing.T) {
	c := New(&latentMount{reply: []byte("12:00:00#"), skips: 5}, time.Second)
	ra, err := c.RA()
	if err != nil || math.Abs(ra-12) > 1e-6 {
		t.Errorf("latent RA = %v, %v", ra, err)
	}
}

// errMount injects write/read errors.
type errMount struct{ writeErr, readErr error }

func (m *errMount) Write(p []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(p), nil
}
func (m *errMount) Read(p []byte) (int, error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	return 0, nil
}
func (m *errMount) Close() error { return nil }

func TestWriteError(t *testing.T) {
	boom := errors.New("boom")
	c := New(&errMount{writeErr: boom}, time.Second)
	if err := c.Halt(); !errors.Is(err, boom) {
		t.Errorf("Halt write error = %v, want boom", err)
	}
	if _, err := c.RA(); !errors.Is(err, boom) {
		t.Errorf("RA write error = %v, want boom", err)
	}
}

func TestReadEOF(t *testing.T) {
	c := New(&errMount{readErr: io.EOF}, time.Second)
	if _, err := c.RA(); err != io.EOF {
		t.Errorf("RA read error = %v, want io.EOF", err)
	}
}

// TestConcurrentCommands runs many commands against one Conn concurrently; the
// command mutex must serialize transport access (run under -race).
func TestConcurrentCommands(t *testing.T) {
	c, _ := newFake(map[string]string{
		":GR#": "12:00:00#",
		":GD#": "+45*00:00#",
	})
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if v, err := c.RA(); err != nil || math.Abs(v-12) > 1e-6 {
					errs <- errors.New("RA wrong/err")
				}
			} else {
				if v, err := c.Dec(); err != nil || math.Abs(v-45) > 1e-6 {
					errs <- errors.New("Dec wrong/err")
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent command: %v", err)
	}
}
