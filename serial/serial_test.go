package serial

import "testing"

func TestOpenError(t *testing.T) {
	if _, err := Open("/dev/nonexistent-lx200-port-zzz", 9600); err == nil {
		t.Errorf("Open nonexistent port: want error")
	}
}

func TestList(t *testing.T) {
	// Enumeration must not error on any platform (the list may be empty).
	if _, err := List(); err != nil {
		t.Errorf("List: %v", err)
	}
}
