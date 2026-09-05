// Package lx200test provides a scripted transport for mount protocol tests.
package lx200test

import (
	"sync"

	"github.com/mikefsq/lx200"
)

// Fake queues scripted replies for written commands. Empty reads return (0, nil).
// All methods are safe for concurrent use.
type Fake struct {
	mu      sync.Mutex
	replies map[string]string
	writes  []string
	rbuf    []byte
}

// New returns a Fake scripted with the given command→reply map.
func New(replies map[string]string) *Fake { return &Fake{replies: replies} }

func (f *Fake) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := string(p)
	f.writes = append(f.writes, cmd)
	if r, ok := f.replies[cmd]; ok {
		f.rbuf = append(f.rbuf, r...)
	}
	return len(p), nil
}

func (f *Fake) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rbuf) == 0 {
		return 0, nil
	}
	n := copy(p, f.rbuf)
	f.rbuf = f.rbuf[n:]
	return n, nil
}

func (f *Fake) Close() error { return nil }

// SetReply changes (or adds) the scripted reply for cmd — e.g. to model a mount
// whose status changes between two reads of the same command.
func (f *Fake) SetReply(cmd, reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.replies == nil {
		f.replies = map[string]string{}
	}
	f.replies[cmd] = reply
}

// Push queues bytes for the mount to "send" unprompted — e.g. RST's asynchronous
// slew-completion token (:MM0#).
func (f *Fake) Push(reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rbuf = append(f.rbuf, reply...)
}

// LastWrite returns the most recently written command ("" if none).
func (f *Fake) LastWrite() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) == 0 {
		return ""
	}
	return f.writes[len(f.writes)-1]
}

// Writes returns a copy of every command written so far.
func (f *Fake) Writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

// Count returns how many times cmd has been written.
func (f *Fake) Count(cmd string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, w := range f.writes {
		if w == cmd {
			n++
		}
	}
	return n
}

// Reset clears the recorded writes (the scripted replies are kept).
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = nil
}

var _ lx200.Transport = (*Fake)(nil)
