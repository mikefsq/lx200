package rst

import "fmt"

// SelectMessier loads Messier object n into the goto target (:LM#).
func (m *Mount) SelectMessier(n int) error { return m.Blind(fmt.Sprintf(":LM%d#", n)) }

// SelectNGC loads NGC object n into the goto target (:LC#).
func (m *Mount) SelectNGC(n int) error { return m.Blind(fmt.Sprintf(":LC%d#", n)) }

// SelectStar loads star n into the goto target (:LS#).
func (m *Mount) SelectStar(n int) error { return m.Blind(fmt.Sprintf(":LS%d#", n)) }

// Find starts an object search (:LF#) using the parameters in browse.go. What it does with the
// result is not established.
func (m *Mount) Find() error { return m.Blind(":LF#") }

// FindAlt sends :Lf#, which calls the same firmware routine as :LF#.
func (m *Mount) FindAlt() error { return m.Blind(":Lf#") }

// SelectLibrary sets the object-library selector (:Lo#). Which library each value selects is
// not established.
func (m *Mount) SelectLibrary(n int) error { return m.Blind(fmt.Sprintf(":Lo%d#", n)) }

// SelectStarCatalog sets the star-catalogue selector (:Ls#). The mapping from value to
// catalogue is not established.
func (m *Mount) SelectStarCatalog(n int) error { return m.Blind(fmt.Sprintf(":Ls%d#", n)) }
