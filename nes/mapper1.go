package nes

import (
	"log"
)

// Mapper1 implements the MMC1 mapper.
//
// http://wiki.nesdev.com/w/index.php/MMC1
type Mapper1 struct {
	*Cartridge

	shiftRegisterCount int
	shiftRegister byte

	// PRG bank modes:
	// 0/1: switchable 32KB @ 0x8000
	// 2: 0x8000: fixed to first bank, 0xC000: switchable 16KB
	// 3: 0x8000: switchable 16KB, 0xC000: fixed to last bank
	prgBankMode int
	prgBank     int

	chr8kMode     bool
	chrBank       [2]int
	chrBankOffset [2]uint16
}

func NewMapper1(cart *Cartridge) *Mapper1 {
	var m *Mapper1 = &Mapper1{Cartridge: cart}

	m.shiftRegisterCount = 0
	m.prgBankMode = 3

	return m
}

func (m *Mapper1) Read(address uint16, isPPU bool) byte {
	if isPPU {
		if m.chr8kMode {
			if address < 0x2000 {
				return m.CHR[m.chrBank[0]][address]
			}
		} else {
			if address < 0x1000 {
				return m.CHR[m.chrBank[0]][m.chrBankOffset[0]+address]
			} else if address < 0x2000 {
				return m.CHR[m.chrBank[1]][m.chrBankOffset[1]+address-0x1000]
			}
		}

		log.Fatalf("Unmapped ReadMem address=%x (isPPU)\n", address)
	}

	if address >= 0x6000 && address <= 0x7FFF {
		return m.SRAM[0][address-0x6000]
	}

	if address < 0x6000 {
		log.Fatalf("Unmapped ReadMem address=%x (!isPPU)\n", address)
	}

	var result byte

	switch m.prgBankMode {
	case 0, 1:
		if address < 0xC000 {
			result = m.PRG[m.prgBank&^1][address-0x8000]
		} else {
			result = m.PRG[m.prgBank|1][address-0xC000]
		}
	case 2:
		if address < 0xC000 {
			result = m.PRG[0][address-0x8000]
		} else {
			result = m.PRG[m.prgBank][address-0xC000]
		}
	case 3:
		if address < 0xC000 {
			result = m.PRG[m.prgBank][address-0x8000]
		} else {
			result = m.PRG[len(m.PRG)-1][address-0xC000]
		}
	}

	return result
}

func (m *Mapper1) Write(address uint16, value byte, isPPU bool) {
	if isPPU {
		if address < 0x2000 {
			if m.chr8kMode {
				if address < 0x2000 {
					m.CHR[m.chrBank[0]][address] = value
				}
			} else {
				if address < 0x1000 {
					m.CHR[m.chrBank[0]][m.chrBankOffset[0]+address] = value
				} else if address < 0x2000 {
					m.CHR[m.chrBank[1]][m.chrBankOffset[1]+address-0x1000] = value
				}
			}
		} else {
			log.Printf("Ignored write to %x (value=%d, isPPU=%v)\n",
				address, value, isPPU)
		}
	} else {
		if address >= 0x6000 && address < 0x8000 {
			m.SRAM[0][address&0x1FFF] = value
		} else if address >= 0x8000 {
			if (value & 0x80) != 0 {
				m.shiftRegisterCount = 0
			} else {
				m.shiftRegisterCount++

				m.shiftRegister >>= 1
				m.shiftRegister |= (value & 1) << 4

				if m.shiftRegisterCount == 5 {
					switch address & 0xE000 {
					case 0x8000:
						switch m.shiftRegister & 0x3 {
						case 0:
							m.Mirror = singleLow
						case 1:
							m.Mirror = singleHigh
						case 2:
							m.Mirror = vertical
						case 3:
							m.Mirror = horizontal
						}

						m.chr8kMode = (m.shiftRegister & 0x10) == 0
						m.prgBankMode = int((m.shiftRegister & 0xC) >> 2)
					case 0xA000:
						m.chrBank[0] = int((m.shiftRegister & 0x1E) >> 1)
						m.chrBankOffset[0] = uint16(m.shiftRegister & 0x1) * 0x1000
					case 0xC000:
						m.chrBank[1] = int((m.shiftRegister & 0x1E) >> 1)
						m.chrBankOffset[1] = uint16(m.shiftRegister & 0x1) * 0x1000
					case 0xE000:
						m.prgBank = int(m.shiftRegister & 0xF)
					}

					m.shiftRegisterCount = 0
				}
			}
		} else {
			log.Printf("Ignored write to %x (value=%d, isPPU=%v)\n",
				address, value, isPPU)
		}
	}
}

func (m *Mapper1) IRQ() bool {
	return false
}

func (m *Mapper1) NextScanline() {
}
