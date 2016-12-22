package nes

// Mapper4 implements the MMC3 mapper.
//
// The MMC3 mapper implements PRG/CHG bank switching and scanline counting.
//
// http://wiki.nesdev.com/w/index.php/MMC3
type Mapper4 struct {
	*Cartridge
	ram [8192]byte

	prgBank       [4]int
	prgBankOffset [4]uint16

	chgBank       [8]int
	chgBankOffset [8]uint16

	bankRegisters        [8]byte
	selectedBankRegister int

	prgBankSwap  bool
	chrInversion bool

	irqEnable        bool
	irqReloadPending bool
	irqLatch         byte
	irqCounter       byte
	irqAssert        bool
}

func NewMapper4(cart *Cartridge) *Mapper4 {
	var m *Mapper4 = &Mapper4{Cartridge: cart}

	m.setPRGBank(0, 0)
	m.setPRGBank(1, 0)
	m.setPRGBank(2, -2)
	m.setPRGBank(3, -1)

	m.irqEnable = true

	return m
}

func (m *Mapper4) setPRGBank(index int, bank int) {
	numPRGBanks := len(m.PRG)

	if bank < 0 {
		bank += numPRGBanks * 2
	}

	m.prgBank[index] = bank >> 1

	var offset uint16 = 0
	if bank&0x1 != 0 {
		offset = 0x2000
	}
	m.prgBankOffset[index] = offset
}

func (m *Mapper4) setCHRBank(index int, bank int) {
	numCHRBanks := len(m.CHR)

	if bank < 0 {
		bank += numCHRBanks * 8
	}

	m.chgBank[index] = bank >> 3

	var offset uint16 = uint16((bank & 0x7) * 0x400)
	m.chgBankOffset[index] = offset
}

func (m *Mapper4) NextScanline() {
	if m.irqCounter == 0 || m.irqReloadPending {
		m.irqCounter = m.irqLatch
		m.irqReloadPending = false
	} else {
		m.irqCounter--
	}

	if m.irqCounter == 0 && m.irqEnable {
		m.irqAssert = true
	}
}

func (m *Mapper4) IRQ() bool {
	isIRQ := m.irqAssert
	m.irqAssert = false

	return isIRQ
}

func (m *Mapper4) Read(address uint16, isPPU bool) byte {
	var result byte = 0

	if isPPU {
		switch {
		case address <= 0x1FFF:
			bank := (address & 0x1C00) >> 10
			offset := address & 0x3FF
			result = m.CHR[m.chgBank[bank]][m.chgBankOffset[bank]+offset]
		default:
			result = 0
		}
	} else {
		switch {
		case address >= 0x6000 && address <= 0x7FFF:
			result = m.ram[address-0x6000]
		case address >= 0x8000 && address <= 0xFFFF:
			bank := (address & 0x6000) >> 13
			offset := address & 0x1FFF
			result = m.PRG[m.prgBank[bank]][m.prgBankOffset[bank]+offset]
		}
	}

	return result
}

func (m *Mapper4) Write(address uint16, value byte, isPPU bool) {
	if !isPPU {
		isEven := address&0x1 == 0

		switch {
		case address >= 0x6000 && address <= 0x7FFF:
			m.ram[address-0x6000] = value
		case address >= 0x8000 && address <= 0x9FFF:
			if isEven {
				m.selectedBankRegister = int(value & 0x7)
				m.prgBankSwap = value&0x40 != 0
				m.chrInversion = value&0x80 != 0
				m.updateMappings()
			} else {
				m.bankRegisters[m.selectedBankRegister] = value
				m.updateMappings()
			}
		case address >= 0xA000 && address <= 0xBFFF:
			if isEven {
				if value&0x1 == 0 {
					m.Mirror = vertical
				} else {
					m.Mirror = horizontal
				}
			} else {
				// PRG RAM protect not implemented.
			}
		case address >= 0xC000 && address <= 0xEFFF:
			if isEven {
				m.irqLatch = value
			} else {
				m.irqReloadPending = true
				m.irqCounter = 0
			}
		case address >= 0xE000 && address <= 0xFFFF:
			if isEven {
				m.irqEnable = false
				m.irqAssert = false
			} else {
				m.irqEnable = true
			}
		}
	}
}

func (m *Mapper4) updateMappings() {
	if m.prgBankSwap {
		m.setPRGBank(0, -2)
		m.setPRGBank(1, int(m.bankRegisters[7]))
		m.setPRGBank(2, int(m.bankRegisters[6]))
		m.setPRGBank(3, -1)
	} else {
		m.setPRGBank(0, int(m.bankRegisters[6]))
		m.setPRGBank(1, int(m.bankRegisters[7]))
		m.setPRGBank(2, -2)
		m.setPRGBank(3, -1)
	}

	if m.chrInversion {
		m.setCHRBank(0, int(m.bankRegisters[2]))
		m.setCHRBank(1, int(m.bankRegisters[3]))
		m.setCHRBank(2, int(m.bankRegisters[4]))
		m.setCHRBank(3, int(m.bankRegisters[5]))
		m.setCHRBank(4, int(m.bankRegisters[0]&0xFE))
		m.setCHRBank(5, int(m.bankRegisters[0]|0x01))
		m.setCHRBank(6, int(m.bankRegisters[1]&0xFE))
		m.setCHRBank(7, int(m.bankRegisters[1]|0x01))
	} else {
		m.setCHRBank(0, int(m.bankRegisters[0]&0xFE))
		m.setCHRBank(1, int(m.bankRegisters[0]|0x01))
		m.setCHRBank(2, int(m.bankRegisters[1]&0xFE))
		m.setCHRBank(3, int(m.bankRegisters[1]|0x01))
		m.setCHRBank(4, int(m.bankRegisters[2]))
		m.setCHRBank(5, int(m.bankRegisters[3]))
		m.setCHRBank(6, int(m.bankRegisters[4]))
		m.setCHRBank(7, int(m.bankRegisters[5]))
	}
}
