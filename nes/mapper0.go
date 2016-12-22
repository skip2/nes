package nes

import (
	"log"
)

// Mapper0 implements the NROM mapper.
//
// http://wiki.nesdev.com/w/index.php/NROM
type Mapper0 struct {
	*Cartridge
	prgBank1 int
	prgBank2 int
}

func NewMapper0(cart *Cartridge) *Mapper0 {
	var m *Mapper0 = &Mapper0{Cartridge: cart}

	numPRGBanks := len(cart.PRG)

	switch numPRGBanks {
	case 1:
		m.prgBank1 = 0
		m.prgBank2 = 0
	case 2:
		m.prgBank1 = 0
		m.prgBank2 = 1
	}

	return m
}

func (m *Mapper0) Read(address uint16, isPPU bool) byte {
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
		result = m.PRG[m.prgBank2][address-0xC000]
	case address >= 0x8000:
		result = m.PRG[m.prgBank1][address-0x8000]
	case address >= 0x6000:
		result = m.SRAM[0][address-0x6000]
	default:
		log.Fatalf("Unmapped ReadMem address=%x (!isPPU)\n",
			address)
	}

	return result
}

func (m *Mapper0) Write(address uint16, value byte, isPPU bool) {
	if !isPPU && address >= 0x6000 && address < 0x8000 {
		m.SRAM[0][address-0x6000] = value
	} else if isPPU && address < 0x2000 {
		m.CHR[0][address] = value
	} else {
		log.Printf("Ignored write to %x (value=%d, isPPU=%v)\n",
			address, value, isPPU)
	}
}

func (m *Mapper0) IRQ() bool {
	return false
}

func (m *Mapper0) NextScanline() {
}
