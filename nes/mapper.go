package nes

import (
	"fmt"
)

// A Mapper sits between a cartridge and the console.
//
// Mappers provide functions such as memory bank switching (as a cartridge can
// contain more memory than the NES's address space allows), and scanline
// counting.
//
// http://wiki.nesdev.com/w/index.php/Mapper
type Mapper interface {
	Read(address uint16, isPPU bool) byte
	Write(address uint16, value byte, isPPU bool)
	IRQ() bool
	NextScanline()
}

// NewMapper returns a mapper of type id for cart.
//
// Each cartridge requires a specific mapper id, which is stated in the iNES
// file header.
//
// The following mappers are currently implemented:
// - 0 (NROM)
// - 1 (MMC1)
// - 2 (UNROM)
// - 4 (MMC3)
//
// An error is returned if the requested mapper id is not implemented.
func NewMapper(id int, cart *Cartridge) (Mapper, error) {
	var mapper Mapper

	switch id {
	case 0:
		mapper = NewMapper0(cart)
	case 1:
		mapper = NewMapper1(cart)
	case 2:
		mapper = NewMapper2(cart)
	case 4:
		mapper = NewMapper4(cart)
	default:
		mapper = nil
	}

	if mapper == nil {
		return nil, fmt.Errorf("mapper ID %d not implemented", id)
	}

	return mapper, nil
}
