package nes

import (
	"testing"
)

func TestMapper0CHR(t *testing.T) {
	cart := NewCartridge(1, 1, 1)
	for i := 0x0000; i < 0x2000; i++ {
		cart.CHR[0][i] = byte(i % 256)
	}

	m := NewMapper0(cart)

	var addr uint16
	for addr = 0x0000; addr < 0x2000; addr++ {
		if m.Read(addr, true) != byte(addr%256) {
			t.Fatalf("Read incorrect @ %x\n", addr)
		}
	}
}

func TestMapper0DoublePRG(t *testing.T) {
	cart := NewCartridge(2, 1, 1)
	cart.PRG[0][0] = 1
	cart.PRG[1][0] = 2

	m := NewMapper0(cart)

	if m.Read(0x8000, false) != 1 {
		t.Fatalf("Read incorrect @ 0x8000\n")
	}

	if m.Read(0xC000, false) != 2 {
		t.Fatalf("Read incorrect @ 0xC000\n")
	}
}

func TestMapper0SRAMReadWrite(t *testing.T) {
	cart := NewCartridge(1, 1, 1)
	m := NewMapper0(cart)

	var addr uint16
	for addr = 0x6000; addr < 0x8000; addr++ {
		m.Write(addr, byte(addr%256), false)
	}

	for addr = 0x6000; addr < 0x8000; addr++ {
		if m.Read(addr, false) != byte(addr%256) {
			t.Fatalf("Read incorrect @ %x\n", addr)
		}
	}
}

func TestMapper0SinglePRG(t *testing.T) {
	cart := NewCartridge(1, 1, 1)
	for i := 0x8000; i < 0xC000; i++ {
		cart.PRG[0][i-0x8000] = byte(i % 256)
	}

	m := NewMapper0(cart)

	var addr uint16
	for addr = 0x8000; addr < 0xC000; addr++ {
		if m.Read(addr, false) != byte(addr%256) {
			t.Fatalf("Read incorrect @ %x\n", addr)
		}
	}

	// Check 0xC000-0xFFFF mirrors 0x8000-0x7FFF.
	for addr = 0xFFFF; addr >= 0xC000; addr-- {
		if m.Read(addr, false) != byte(addr%256) {
			t.Fatalf("Read incorrect @ %x\n", addr)
		}
	}
}
