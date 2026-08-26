package rst

import (
	"fmt"
	"strings"
)

// Site slots (:W). The mount stores several observing sites; a slot index selects which one
// :Sg and :St write into and which :GM#, :GN# and :GO# name.

// SiteSlot reads the selected site slot (:WA#).
func (m *Mount) SiteSlot() (string, error) {
	s, err := m.Get(":WA#")
	return strings.TrimSpace(s), err
}

// SelectSiteSlot selects the slot that :Sg and :St write into (:W<n>#). Silent.
func (m *Mount) SelectSiteSlot(n int) error { return m.Blind(fmt.Sprintf(":W%d#", n)) }

// SelectSiteSlotA sends the lowercase :Wa# branch, which the firmware tests separately from
// the digit form. Its slot arithmetic yields 49 for 'a' rather than a slot number, so it is
// exposed only to make the branch reachable.
func (m *Mount) SelectSiteSlotA() error { return m.Blind(":Wa#") }

// SiteStatusQ reads :WQ#, a single digit whose meaning is not established.
func (m *Mount) SiteStatusQ() (string, error) {
	s, err := m.Get(":WQ#")
	return strings.TrimSpace(s), err
}

// SiteInfoI reads :WI#. It answers with an empty payload on the development mount.
func (m *Mount) SiteInfoI() (string, error) {
	s, err := m.Get(":WI#")
	return strings.TrimSpace(s), err
}

// SiteInfoJ reads :WJ#, as SiteInfoI.
func (m *Mount) SiteInfoJ() (string, error) {
	s, err := m.Get(":WJ#")
	return strings.TrimSpace(s), err
}

// SetSiteValueL sends :WL# or :Wl# with a 4-digit argument. What it configures is not
// established.
func (m *Mount) SetSiteValueL(v int, upper bool) error { return m.wSet('L', 'l', v, upper) }

// SetSiteValueM sends :WM# or :Wm#, as SetSiteValueL. Both pairs write a shared global.
func (m *Mount) SetSiteValueM(v int, upper bool) error { return m.wSet('M', 'm', v, upper) }

func (m *Mount) wSet(up, low byte, v int, upper bool) error {
	c := low
	if upper {
		c = up
	}
	return m.Blind(fmt.Sprintf(":W%c%04d#", c, v))
}
