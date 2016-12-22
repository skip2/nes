package nes

import (
	"log"
)

// Mapper2 implements the UNROM mapper.
//
// http://wiki.nesdev.com/w/index.php/UxROM
type Mapper2 struct {
	*Cartridge
	prgSwitchableBank int
	prgLastBank       int
}

func NewMapper2(cart *Cartridge) *Mapper2 {
	var m *Mapper2 = &Mapper2{Cartridge: cart}

	m.prgSwitchableBank = 0
	m.prgLastBank = len(m.PRG) - 1

	return m
}

func (m *Mapper2) Read(address uint16, isPPU bool) byte {
	if isPPU {
		if address < 0x2000 {
			return m.CHR[0][address]
		} else {
			log.Fatalf("Unmapped ReadMem address=%x (isPPU)\n",
				address)
		}
	}

	var result byte

	switch {
	case address >= 0xC000:
		result = m.PRG[m.prgLastBank][address-0xC000]
	case address >= 0x8000:
		result = m.PRG[m.prgSwitchableBank][address-0x8000]
	default:
		log.Fatalf("Unmapped ReadMem address=%x (!isPPU)\n",
			address)
	}

	return result
}

func (m *Mapper2) Write(address uint16, value byte, isPPU bool) {
	if isPPU && address < 0x2000 {
		m.CHR[0][address] = value
	} else if !isPPU && address >= 0x8000 {
		m.prgSwitchableBank = int(value & 0x7)
	} else {
		log.Printf("Ignored write to %x (value=%d, isPPU=%v)\n",
			address, value, isPPU)
	}
}

func (m *Mapper2) IRQ() bool {
	return false
}

func (m *Mapper2) NextScanline() {
}
