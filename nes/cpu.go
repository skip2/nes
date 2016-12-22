package nes

import (
	"fmt"
	"log"
)

// Interrupt vectors && stack base address.
const StackBase uint16 = 0x100
const ResetVector uint16 = 0xfffc
const InterruptVector uint16 = 0xfffe
const NMIVector uint16 = 0xfffa

// CPU emulates a NES 6502 CPU.
//
// https://web.archive.org/web/20110320213225/http://www.obelisk.demon.co.uk/6502/
type CPU struct {
	Console *Console
	RAM     [2048]byte

	NumCycles uint64
	PC        uint16
	SP        byte
	A         byte
	X         byte
	Y         byte

	flagCarry            bool
	flagZero             bool
	flagInterruptDisable bool
	flagDecimalMode      bool
	flagBreak            bool
	flagOverflow         bool
	flagSign             bool

	instructions [256]instruction
}

// Instruction represents a single CPU instruction type.
//
// Each instruction may have multiple variants, each with a different memory
// addressing mode.
type instruction struct {
	Name               string
	Impl               func(value uint16) int
	Size               uint16
	NumBaseCycles      int
	NumPageCrossCycles int
	GetAddressImpl     func() (uint16, bool)
}

// NewCPU constructs and returns a CPU for the given console.
func NewCPU(console *Console) *CPU {
	c := &CPU{Console: console,
		SP:                   0xFD,
		flagInterruptDisable: true}

	c.loadInstructions()
	c.PC = c.read16(ResetVector)

	return c
}

// String returns the CPU state as a string.
func (c *CPU) String() string {
	instructionBytes, _ := c.NextInstructionBytes()

	var result string
	result = fmt.Sprintf("PC=%04X A=%02X X=%02X Y=%02X P=%02X SP=%02X, ins=% X, ",
		c.PC,
		c.A,
		c.X,
		c.Y,
		c.P(),
		c.SP,
		instructionBytes)

	result += fmt.Sprintf("C=%v Z=%v I=%v D=%v B=%v O=%v S=%v\n",
		c.flagCarry,
		c.flagZero,
		c.flagInterruptDisable,
		c.flagDecimalMode,
		c.flagBreak,
		c.flagOverflow,
		c.flagSign)

	return result
}

// Step runs the CPU for one step.
//
// Normally this is one instruction, but multiple instructions may be executed
// if an IRQ is handled.
//
// Returns the total number of CPU cycles executed in the lifetime of the CPU,
// starting from 0.
func (c *CPU) Step() (uint64, error) {
	var numCycles int = 0

	if !c.flagInterruptDisable {
		if c.Console.Cart.IRQ() {
			numCycles += c.interrupt()
		}
	}

	var opcode byte = c.read(c.PC)
	var instruction *instruction = &c.instructions[opcode]

	if instruction.Size == 0 {
		return 0, fmt.Errorf("invalid instruction %x @ PC=%x",
			opcode, c.PC)
	}

	numCycles += instruction.NumBaseCycles

	var value uint16
	var pageCrossed bool
	value, pageCrossed = instruction.GetAddressImpl()

	c.PC += instruction.Size

	if pageCrossed {
		numCycles += instruction.NumPageCrossCycles
	}

	var extraCycles int = instruction.Impl(value)

	if extraCycles == -1 {
		return c.NumCycles, fmt.Errorf("unimplemented %s", instruction.Name)
	}

	c.NumCycles += uint64(numCycles + extraCycles)
	return c.NumCycles, nil
}

func (c *CPU) pagesEqual(p1 uint16, p2 uint16) bool {
	return p1&0xFF00 == p2&0xFF00
}

func (c *CPU) adc(address uint16) int {
	var value byte = c.read(address)

	var carry byte = 0
	if c.flagCarry {
		carry = 1
	}

	c.flagCarry = (int(c.A) + int(value) + int(carry)) > 0xFF

	aSign := signBitSet(c.A)
	valueSign := signBitSet(byte(value))

	c.A = c.A + byte(value) + carry
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)

	resultSign := signBitSet(c.A)

	c.flagOverflow = (aSign && valueSign && !resultSign) ||
		(!aSign && !valueSign && resultSign)

	return 0
}

func (c *CPU) interrupt() int {
	c.push16(c.PC)
	c.push8(c.P())

	c.PC = c.read16(InterruptVector)
	c.flagInterruptDisable = true

	return 7
}

// NMI starts a non-maskable interrupt.
func (c *CPU) NMI() int {
	c.push16(c.PC)
	c.push8(c.P())

	c.PC = c.read16(NMIVector)
	c.flagInterruptDisable = true

	return 7
}

func signBitSet(value byte) bool {
	return value&0x80 != 0
}

func (c *CPU) and(address uint16) int {
	var value byte = c.read(address)

	c.A = c.A & value
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) asl(address uint16) int {
	var value byte = c.read(address)

	c.flagCarry = value&0x80 != 0
	value <<= 1
	c.updateflagZero(value)
	c.updateflagSign(value)

	cycles := c.write(address, value)

	return cycles
}

func (c *CPU) asla(address uint16) int {
	c.flagCarry = c.A&0x80 != 0
	c.A <<= 1
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) bcc(address uint16) int {
	if !c.flagCarry {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) bcs(address uint16) int {
	if c.flagCarry {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) beq(address uint16) int {
	if c.flagZero {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) bit(address uint16) int {
	var value byte = c.read(address)
	var result byte = c.A & value

	c.updateflagZero(result)
	c.flagOverflow = value&0x40 != 0
	c.flagSign = value&0x80 != 0

	return 0
}

func (c *CPU) doBranch(address uint16) int {
	cycles := 1

	if !c.pagesEqual(c.PC, address) {
		cycles++
	}

	c.PC = address

	return cycles
}

func (c *CPU) bmi(address uint16) int {
	if c.flagSign {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) bne(address uint16) int {
	if !c.flagZero {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) bpl(address uint16) int {
	if !c.flagSign {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) brk(address uint16) int {
	c.push16(c.PC + 1)

	c.flagBreak = true
	c.push8(c.P())

	c.PC = c.read16(InterruptVector)
	c.flagInterruptDisable = true
	return 0
}

func (c *CPU) bvc(address uint16) int {
	if !c.flagOverflow {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) bvs(address uint16) int {
	if c.flagOverflow {
		return c.doBranch(address)
	}

	return 0
}

func (c *CPU) clc(address uint16) int {
	c.flagCarry = false
	return 0
}
func (c *CPU) cld(address uint16) int {
	c.flagDecimalMode = false
	return 0
}

func (c *CPU) cli(address uint16) int {
	c.flagInterruptDisable = false
	return 0
}

func (c *CPU) clv(address uint16) int {
	c.flagOverflow = false
	return 0
}

func (c *CPU) cmp(address uint16) int {
	var value byte = c.read(address)
	c.compare(c.A, value)
	return 0
}

func (c *CPU) cpx(address uint16) int {
	var value byte = c.read(address)
	c.compare(c.X, value)
	return 0
}

func (c *CPU) cpy(address uint16) int {
	var value byte = c.read(address)
	c.compare(c.Y, value)
	return 0
}

func (c *CPU) dec(address uint16) int {
	var value byte = c.read(address)
	value--
	cycles := c.write(address, value)
	c.updateflagZero(value)
	c.updateflagSign(value)
	return cycles
}

func (c *CPU) dex(address uint16) int {
	c.X--
	c.updateflagZero(c.X)
	c.updateflagSign(c.X)
	return 0
}

func (c *CPU) dey(address uint16) int {
	c.Y--
	c.updateflagZero(c.Y)
	c.updateflagSign(c.Y)
	return 0
}

func (c *CPU) eor(address uint16) int {
	var value byte = c.read(address)
	c.A = c.A ^ value
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) inc(address uint16) int {
	var value byte = c.read(address)
	value++
	cycles := c.write(address, value)
	c.updateflagZero(value)
	c.updateflagSign(value)
	return cycles
}

func (c *CPU) inx(address uint16) int {
	c.X++
	c.updateflagZero(c.X)
	c.updateflagSign(c.X)
	return 0
}
func (c *CPU) iny(address uint16) int {
	c.Y++
	c.updateflagZero(c.Y)
	c.updateflagSign(c.Y)
	return 0
}

func (c *CPU) jmp(address uint16) int {
	c.PC = address
	return 0
}

func (c *CPU) jsr(address uint16) int {
	c.push16(c.PC - 1)
	c.PC = address
	return 0
}

func (c *CPU) lda(address uint16) int {
	var value byte = c.read(address)

	c.A = value
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) ldx(address uint16) int {
	var value byte = c.read(address)

	c.X = value
	c.updateflagZero(c.X)
	c.updateflagSign(c.X)
	return 0
}

func (c *CPU) ldy(address uint16) int {
	var value byte = c.read(address)

	c.Y = value
	c.updateflagZero(c.Y)
	c.updateflagSign(c.Y)
	return 0
}

func (c *CPU) lsr(address uint16) int {
	var value byte = c.read(address)

	c.flagCarry = value&0x01 != 0
	value >>= 1
	c.updateflagZero(value)
	c.updateflagSign(value)

	cycles := c.write(address, value)

	return cycles
}

func (c *CPU) lsra(address uint16) int {
	c.flagCarry = c.A&0x01 != 0
	c.A >>= 1
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) nop(address uint16) int {
	return 0
}

func (c *CPU) ora(address uint16) int {
	var value byte = c.read(address)

	c.A = c.A | value
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) pha(address uint16) int {
	c.push8(c.A)
	return 0
}

func (c *CPU) php(address uint16) int {
	c.push8(c.P() | 0x10)
	return 0
}

func (c *CPU) pla(address uint16) int {
	c.A = c.pop8()
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) plp(address uint16) int {
	p := c.pop8() & 0xEF

	c.flagCarry = p&0x01 != 0
	c.flagZero = p&0x02 != 0
	c.flagInterruptDisable = p&0x04 != 0
	c.flagDecimalMode = p&0x08 != 0
	c.flagBreak = p&0x10 != 0
	c.flagOverflow = p&0x40 != 0
	c.flagSign = p&0x80 != 0

	return 0
}

func (c *CPU) rol(address uint16) int {
	var value byte = c.read(address)
	c.rolImpl(&value)
	cycles := c.write(address, value)
	return cycles
}

func (c *CPU) rola(address uint16) int {
	c.rolImpl(&c.A)
	return 0
}

func (c *CPU) rolImpl(value *byte) {
	var newflagCarry bool = *value&0x80 != 0

	*value <<= 1
	if c.flagCarry {
		*value |= 0x01
	}

	c.flagCarry = newflagCarry

	c.updateflagZero(*value)
	c.updateflagSign(*value)
}

func (c *CPU) ror(address uint16) int {
	var value byte = c.read(address)
	c.rorImpl(&value)
	cycles := c.write(address, value)
	return cycles
}

func (c *CPU) rora(address uint16) int {
	c.rorImpl(&c.A)
	return 0
}

func (c *CPU) rorImpl(value *byte) {
	var newflagCarry bool = *value&0x1 != 0

	*value >>= 1
	if c.flagCarry {
		*value |= 0x80
	}

	c.flagCarry = newflagCarry

	c.updateflagZero(*value)
	c.updateflagSign(*value)
}

func (c *CPU) rti(address uint16) int {
	c.plp(0)
	c.PC = c.pop16()
	return 0
}

func (c *CPU) rts(address uint16) int {
	c.PC = c.pop16() + 1
	return 0
}

func (c *CPU) sbc(address uint16) int {
	var value byte = c.read(address)

	var carry byte = 0
	if !c.flagCarry {
		carry = 1
	}

	c.flagCarry = (int(c.A) - int(value) - int(carry)) >= 0

	aSign := signBitSet(c.A)
	valueSign := !signBitSet(byte(value))

	c.A = c.A - byte(value) - carry
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)

	resultSign := signBitSet(c.A)

	c.flagOverflow = (aSign && valueSign && !resultSign) ||
		(!aSign && !valueSign && resultSign)

	return 0
}

func (c *CPU) sec(address uint16) int {
	c.flagCarry = true
	return 0
}

func (c *CPU) sed(address uint16) int {
	c.flagDecimalMode = true
	return 0
}

func (c *CPU) sei(address uint16) int {
	c.flagInterruptDisable = true
	return 0
}

func (c *CPU) sta(address uint16) int {
	cycles := c.write(address, c.A)
	return cycles
}

func (c *CPU) stx(address uint16) int {
	cycles := c.write(address, c.X)
	return cycles
}

func (c *CPU) sty(address uint16) int {
	cycles := c.write(address, c.Y)
	return cycles
}

func (c *CPU) tax(address uint16) int {
	c.X = c.A
	c.updateflagZero(c.X)
	c.updateflagSign(c.X)
	return 0
}

func (c *CPU) tay(address uint16) int {
	c.Y = c.A
	c.updateflagZero(c.Y)
	c.updateflagSign(c.Y)
	return 0
}

func (c *CPU) tsx(address uint16) int {
	c.X = c.SP
	c.updateflagZero(c.X)
	c.updateflagSign(c.X)
	return 0
}

func (c *CPU) txa(address uint16) int {
	c.A = c.X
	c.updateflagZero(c.X)
	c.updateflagSign(c.X)
	return 0
}

func (c *CPU) txs(address uint16) int {
	c.SP = c.X
	return 0
}

func (c *CPU) tya(address uint16) int {
	c.A = c.Y
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) xxx(address uint16) int {
	return 0
}

func (c *CPU) dop(address uint16) int {
	return 0
}

func (c *CPU) top(address uint16) int {
	return 0
}

func (c *CPU) lax(address uint16) int {
	c.A = c.read(address)
	c.X = c.A
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) aax(address uint16) int {
	var value byte = c.A & c.X
	cycles := c.write(address, value)
	return cycles
}

func (c *CPU) dcp(address uint16) int {
	cycles := c.dec(address)
	cycles += c.cmp(address)
	return cycles
}

func (c *CPU) isc(address uint16) int {
	cycles := c.inc(address)
	cycles += c.sbc(address)
	return cycles
}

func (c *CPU) slo(address uint16) int {
	cycles := c.asl(address)
	cycles += c.ora(address)
	return cycles
}

func (c *CPU) rla(address uint16) int {
	cycles := c.rol(address)
	cycles += c.and(address)
	return cycles
}

func (c *CPU) sre(address uint16) int {
	cycles := c.lsr(address)
	cycles += c.eor(address)
	return cycles
}

func (c *CPU) rra(address uint16) int {
	cycles := c.ror(address)
	cycles += c.adc(address)
	return cycles
}

func (c *CPU) anc(address uint16) int {
	cycles := c.and(address)
	c.flagCarry = c.flagSign
	return cycles
}

func (c *CPU) alr(address uint16) int {
	cycles := c.and(address)
	cycles += c.lsra(address)
	return cycles
}

func (c *CPU) arr(address uint16) int {
	value := c.read(address)

	c.A = (c.A & value) >> 1
	if c.flagCarry {
		c.A |= 0x80
	}

	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	c.flagCarry = (c.A>>6)&0x1 != 0
	c.flagOverflow = ((c.A>>6)^(c.A>>5))&0x1 != 0
	return 0
}

func (c *CPU) lxa(address uint16) int {
	c.A = c.read(address)
	c.X = c.A
	c.updateflagZero(c.A)
	c.updateflagSign(c.A)
	return 0
}

func (c *CPU) sax(address uint16) int {
	var value byte = c.read(address)
	var d byte = c.A & c.X

	c.flagCarry = (int(d) - int(value)) >= 0

	c.X = d - value
	c.updateflagZero(c.X)
	c.updateflagSign(c.X)
	return 0
}

func (c *CPU) compare(a byte, m byte) {
	result := a - m

	c.updateflagZero(result)
	c.updateflagSign(result)
	c.flagCarry = a >= m
}

func (c *CPU) updateflagZero(value byte) {
	c.flagZero = value == 0
}

func (c *CPU) updateflagSign(value byte) {
	c.flagSign = value&0x80 == 0x80
}

func (c *CPU) getAddrAbsolute() (uint16, bool) {
	return c.read16(c.PC + 1), false
}

func (c *CPU) getAddrAbsoluteX() (uint16, bool) {
	var address uint16 = c.read16(c.PC + 1)
	var finalAddress uint16 = address + uint16(c.X)
	return finalAddress, !c.pagesEqual(address, finalAddress)
}

func (c *CPU) getAddrAbsoluteY() (uint16, bool) {
	var address uint16 = c.read16(c.PC + 1)
	var finalAddress uint16 = address + uint16(c.Y)
	return finalAddress, !c.pagesEqual(address, finalAddress)
}

func (c *CPU) getAddrAccumulator() (uint16, bool) {
	return 0, false
}

func (c *CPU) getAddrImmediate() (uint16, bool) {
	return c.PC + 1, false
}

func (c *CPU) getAddrImplied() (uint16, bool) {
	return 0, false
}

func (c *CPU) getAddrIndirect() (uint16, bool) {
	return c.read16WithPageBoundaryBug(c.read16(c.PC + 1)), false
}

func (c *CPU) getAddrIndirectX() (uint16, bool) {
	from := uint16(c.read(c.PC+1) + c.X)
	var address uint16 = c.read16WithPageBoundaryBug(from)

	return address, false
}

func (c *CPU) read16(address uint16) uint16 {
	var low uint16 = address
	var high uint16 = address + 1

	return uint16(c.read(low)) | uint16(c.read(high))<<8
}

func (c *CPU) read16WithPageBoundaryBug(address uint16) uint16 {
	var low uint16 = address
	var high uint16

	if address&0xFF == 0xFF {
		high = address & 0xFF00
	} else {
		high = address + 1
	}

	return uint16(c.read(low)) | uint16(c.read(high))<<8
}

func (c *CPU) getAddrIndirectY() (uint16, bool) {
	var from uint16 = uint16(c.read(c.PC + 1))
	var address uint16 = c.read16WithPageBoundaryBug(from)
	var finalAddress uint16 = address + uint16(c.Y)

	return finalAddress, !c.pagesEqual(address, finalAddress)
}

func (c *CPU) getAddrRelative() (uint16, bool) {
	address := c.PC + 2

	offset := int8(c.read(c.PC + 1))
	if offset < 0 {
		address -= uint16(-offset)
	} else {
		address += uint16(offset)
	}

	return address, false
}

func (c *CPU) getAddrZeroPage() (uint16, bool) {
	return uint16(c.read(c.PC + 1)), false
}

func (c *CPU) getAddrZeroPageX() (uint16, bool) {
	var address byte = c.read(c.PC+1) + c.X
	return uint16(address), false
}

func (c *CPU) getAddrZeroPageY() (uint16, bool) {
	var address byte = c.read(c.PC+1) + c.Y
	return uint16(address), false
}

func (c *CPU) push8(value byte) {
	c.write(StackBase+uint16(c.SP), value)
	c.SP -= 1
}

func (c *CPU) pop8() byte {
	value := c.read(StackBase + uint16(c.SP) + 1)
	c.SP += 1

	return value
}

func (c *CPU) push16(value uint16) {
	c.write(StackBase+uint16(c.SP)-1, byte(value&0xFF))
	c.write(StackBase+uint16(c.SP), byte(value>>8))
	c.SP -= 2
}

func (c *CPU) pop16() uint16 {
	low := uint16(c.read(StackBase + uint16(c.SP) + 1))
	high := uint16(c.read(StackBase + uint16(c.SP) + 2))
	c.SP += 2

	return high<<8 | low
}

func (c *CPU) P() byte {
	var p byte = 0

	if c.flagCarry {
		p |= 0x01
	}

	if c.flagZero {
		p |= 0x02
	}

	if c.flagInterruptDisable {
		p |= 0x04
	}

	if c.flagDecimalMode {
		p |= 0x08
	}

	if c.flagBreak {
		p |= 0x10
	}

	p |= 0x20

	if c.flagOverflow {
		p |= 0x40
	}

	if c.flagSign {
		p |= 0x80
	}

	return p
}

func (c *CPU) NextInstructionBytes() ([]byte, error) {
	var opcode byte = c.read(c.PC)
	var instruction *instruction = &c.instructions[opcode]

	bytes := make([]byte, 0, 3)

	if instruction.Size == 0 {
		return bytes, fmt.Errorf("invalid instruction %x @ PC=%x",
			opcode, c.PC)
	}

	var i uint16
	for i = 0; i < instruction.Size; i++ {
		bytes = append(bytes, c.read(c.PC+i))
	}

	return bytes, nil
}

func (c *CPU) read(address uint16) byte {
	var result byte

	switch {
	case address < 0x2000:
		result = c.RAM[address&0x7FF]
	case address >= 0x2000 && address < 0x4000:
		switch address & 0x7 {
		case 2:
			result = c.Console.PPU.StatusRegister()
		case 4:
			result = c.Console.PPU.ReadSPR()
		case 7:
			result = c.Console.PPU.ReadData()
		default:
			log.Printf("Unknown read @ %x", address)
		}
	case address == 0x4016:
		result = c.Console.Joypads[0].Read()
	case address == 0x4017:
		result = c.Console.Joypads[1].Read()
	case address >= 0x6000 && address <= 0xFFFF:
		result = c.Console.Cart.Read(address, false)
	default:
		// log.Printf("Unimplemented CPU mem read @ %x", address)
		result = 0xFF
	}

	return result
}

func (c *CPU) write(address uint16, value byte) int {
	cycles := 0

	switch {
	case address < 0x2000:
		c.RAM[address&0x7FF] = value
	case address >= 0x2000 && address < 0x4000:
		switch address & 0x7 {
		case 0x0:
			c.Console.PPU.SetControlRegister(value)
		case 0x1:
			c.Console.PPU.SetMaskRegister(value)
		case 0x3:
			c.Console.PPU.SetSPRAddress(value)
		case 0x4:
			c.Console.PPU.WriteSPR(value)
		case 0x5:
			c.Console.PPU.WriteScroll(value)
		case 0x6:
			c.Console.PPU.WriteDataAddress(value)
		case 0x7:
			c.Console.PPU.WriteData(value)
		default:
			log.Printf("Unknown write @ %x", address)
		}
	case address == 0x4016:
		c.Console.Joypads[0].Write(value)
	case address == 0x4017:
		c.Console.Joypads[1].Write(value)
	case address == 0x4014:
		c.Console.PPU.SetSPRAddress(0)
		var i uint16
		for i = 0; i < 0x100; i++ {
			sprValue := c.read(uint16(value)*0x100 + i)
			c.Console.PPU.WriteSPR(sprValue)
		}
		cycles = 512
	case address >= 0x6000 && address < 0x8000:
		c.Console.Cart.Write(address, value, false)
	case address >= 0x8000 && address <= 0xFFFF:
		c.Console.Cart.Write(address, value, false)
	default:
		// log.Printf("Unimplemented CPU mem write @ %x", address)
	}

	return cycles
}

func (c *CPU) loadInstructions() {
	c.instructions = [256]instruction{
		/* 0x00 */ {"BRK", c.brk, 1, 7, 0, c.getAddrImplied},
		/* 0x01 */ {"ORA", c.ora, 2, 6, 0, c.getAddrIndirectX},
		/* 0x02 */ {"x02", c.xxx, 0, 0, 0, nil},
		/* 0x03 */ {"SLO", c.slo, 2, 8, 0, c.getAddrIndirectX},
		/* 0x04 */ {"DOP", c.dop, 2, 3, 0, c.getAddrZeroPage},
		/* 0x05 */ {"ORA", c.ora, 2, 3, 0, c.getAddrZeroPage},
		/* 0x06 */ {"ASL", c.asl, 2, 5, 0, c.getAddrZeroPage},
		/* 0x07 */ {"SLO", c.slo, 2, 5, 0, c.getAddrZeroPage},
		/* 0x08 */ {"PHP", c.php, 1, 3, 0, c.getAddrImplied},
		/* 0x09 */ {"ORA", c.ora, 2, 2, 0, c.getAddrImmediate},
		/* 0x0A */ {"ASL", c.asla, 1, 2, 0, c.getAddrAccumulator},
		/* 0x0B */ {"ANC", c.anc, 2, 2, 0, c.getAddrImmediate},
		/* 0x0C */ {"TOP", c.top, 3, 4, 0, c.getAddrAbsolute},
		/* 0x0D */ {"ORA", c.ora, 3, 4, 0, c.getAddrAbsolute},
		/* 0x0E */ {"ASL", c.asl, 3, 6, 0, c.getAddrAbsolute},
		/* 0x0F */ {"SLO", c.slo, 3, 6, 0, c.getAddrAbsolute},
		/* 0x10 */ {"BPL", c.bpl, 2, 2, 0, c.getAddrRelative},
		/* 0x11 */ {"ORA", c.ora, 2, 5, 1, c.getAddrIndirectY},
		/* 0x12 */ {"x12", c.xxx, 0, 0, 0, nil},
		/* 0x13 */ {"SLO", c.slo, 2, 8, 0, c.getAddrIndirectY},
		/* 0x14 */ {"DOP", c.dop, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x15 */ {"ORA", c.ora, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x16 */ {"ASL", c.asl, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x17 */ {"SLO", c.slo, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x18 */ {"CLC", c.clc, 1, 2, 0, c.getAddrImplied},
		/* 0x19 */ {"ORA", c.ora, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0x1A */ {"NOP", c.nop, 1, 2, 0, c.getAddrImplied},
		/* 0x1B */ {"SLO", c.slo, 3, 7, 0, c.getAddrAbsoluteY},
		/* 0x1C */ {"TOP", c.top, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x1D */ {"ORA", c.ora, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x1E */ {"ASL", c.asl, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x1F */ {"SLO", c.slo, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x20 */ {"JSR", c.jsr, 3, 6, 0, c.getAddrAbsolute},
		/* 0x21 */ {"AND", c.and, 2, 6, 0, c.getAddrIndirectX},
		/* 0x22 */ {"x22", c.xxx, 0, 0, 0, nil},
		/* 0x23 */ {"RLA", c.rla, 2, 8, 0, c.getAddrIndirectX},
		/* 0x24 */ {"BIT", c.bit, 2, 3, 0, c.getAddrZeroPage},
		/* 0x25 */ {"AND", c.and, 2, 3, 0, c.getAddrZeroPage},
		/* 0x26 */ {"ROL", c.rol, 2, 5, 0, c.getAddrZeroPage},
		/* 0x27 */ {"RLA", c.rla, 2, 5, 0, c.getAddrZeroPage},
		/* 0x28 */ {"PLP", c.plp, 1, 4, 0, c.getAddrImplied},
		/* 0x29 */ {"AND", c.and, 2, 2, 0, c.getAddrImmediate},
		/* 0x2A */ {"ROL", c.rola, 1, 2, 0, c.getAddrAccumulator},
		/* 0x2B */ {"ANC", c.anc, 2, 2, 0, c.getAddrImmediate},
		/* 0x2C */ {"BIT", c.bit, 3, 4, 0, c.getAddrAbsolute},
		/* 0x2D */ {"AND", c.and, 3, 4, 0, c.getAddrAbsolute},
		/* 0x2E */ {"ROL", c.rol, 3, 6, 0, c.getAddrAbsolute},
		/* 0x2F */ {"RLA", c.rla, 3, 6, 0, c.getAddrAbsolute},
		/* 0x30 */ {"BMI", c.bmi, 2, 2, 0, c.getAddrRelative},
		/* 0x31 */ {"AND", c.and, 2, 5, 1, c.getAddrIndirectY},
		/* 0x32 */ {"x32", c.xxx, 0, 0, 0, nil},
		/* 0x33 */ {"RLA", c.rla, 2, 8, 0, c.getAddrIndirectY},
		/* 0x34 */ {"DOP", c.dop, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x35 */ {"AND", c.and, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x36 */ {"ROL", c.rol, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x37 */ {"RLA", c.rla, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x38 */ {"SEC", c.sec, 1, 2, 0, c.getAddrImplied},
		/* 0x39 */ {"AND", c.and, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0x3A */ {"NOP", c.nop, 1, 2, 0, c.getAddrImplied},
		/* 0x3B */ {"RLA", c.rla, 3, 7, 0, c.getAddrAbsoluteY},
		/* 0x3C */ {"TOP", c.top, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x3D */ {"AND", c.and, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x3E */ {"ROL", c.rol, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x3F */ {"RLA", c.rla, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x40 */ {"RTI", c.rti, 1, 6, 0, c.getAddrImplied},
		/* 0x41 */ {"EOR", c.eor, 2, 6, 0, c.getAddrIndirectX},
		/* 0x42 */ {"x42", c.xxx, 0, 0, 0, nil},
		/* 0x43 */ {"SRE", c.sre, 2, 8, 0, c.getAddrIndirectX},
		/* 0x44 */ {"DOP", c.dop, 2, 3, 0, c.getAddrZeroPage},
		/* 0x45 */ {"EOR", c.eor, 2, 3, 0, c.getAddrZeroPage},
		/* 0x46 */ {"LSR", c.lsr, 2, 5, 0, c.getAddrZeroPage},
		/* 0x47 */ {"SRE", c.sre, 2, 5, 0, c.getAddrZeroPage},
		/* 0x48 */ {"PHA", c.pha, 1, 3, 0, c.getAddrImplied},
		/* 0x49 */ {"EOR", c.eor, 2, 2, 0, c.getAddrImmediate},
		/* 0x4A */ {"LSR", c.lsra, 1, 2, 0, c.getAddrAccumulator},
		/* 0x4B */ {"ALR", c.alr, 2, 2, 0, c.getAddrImmediate},
		/* 0x4C */ {"JMP", c.jmp, 3, 3, 0, c.getAddrAbsolute},
		/* 0x4D */ {"EOR", c.eor, 3, 4, 0, c.getAddrAbsolute},
		/* 0x4E */ {"LSR", c.lsr, 3, 6, 0, c.getAddrAbsolute},
		/* 0x4F */ {"SRE", c.sre, 3, 6, 0, c.getAddrAbsolute},
		/* 0x50 */ {"BVC", c.bvc, 2, 2, 0, c.getAddrRelative},
		/* 0x51 */ {"EOR", c.eor, 2, 5, 1, c.getAddrIndirectY},
		/* 0x52 */ {"x52", c.xxx, 0, 0, 0, nil},
		/* 0x53 */ {"SRE", c.sre, 2, 8, 0, c.getAddrIndirectY},
		/* 0x54 */ {"DOP", c.dop, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x55 */ {"EOR", c.eor, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x56 */ {"LSR", c.lsr, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x57 */ {"SRE", c.sre, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x58 */ {"CLI", c.cli, 1, 2, 0, c.getAddrImplied},
		/* 0x59 */ {"EOR", c.eor, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0x5A */ {"NOP", c.nop, 1, 2, 0, c.getAddrImplied},
		/* 0x5B */ {"SRE", c.sre, 3, 7, 0, c.getAddrAbsoluteY},
		/* 0x5C */ {"TOP", c.top, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x5D */ {"EOR", c.eor, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x5E */ {"LSR", c.lsr, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x5F */ {"SRE", c.sre, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x60 */ {"RTS", c.rts, 1, 6, 0, c.getAddrImplied},
		/* 0x61 */ {"ADC", c.adc, 2, 6, 0, c.getAddrIndirectX},
		/* 0x62 */ {"x62", c.xxx, 0, 0, 0, nil},
		/* 0x63 */ {"RRA", c.rra, 2, 8, 0, c.getAddrIndirectX},
		/* 0x64 */ {"DOP", c.dop, 2, 3, 0, c.getAddrZeroPage},
		/* 0x65 */ {"ADC", c.adc, 2, 3, 0, c.getAddrZeroPage},
		/* 0x66 */ {"ROR", c.ror, 2, 5, 0, c.getAddrZeroPage},
		/* 0x67 */ {"RRA", c.rra, 2, 5, 0, c.getAddrZeroPage},
		/* 0x68 */ {"PLA", c.pla, 1, 4, 0, c.getAddrImplied},
		/* 0x69 */ {"ADC", c.adc, 2, 2, 0, c.getAddrImmediate},
		/* 0x6A */ {"ROR", c.rora, 1, 2, 0, c.getAddrAccumulator},
		/* 0x6B */ {"ARR", c.arr, 2, 2, 0, c.getAddrImmediate},
		/* 0x6C */ {"JMP", c.jmp, 3, 5, 0, c.getAddrIndirect},
		/* 0x6D */ {"ADC", c.adc, 3, 4, 0, c.getAddrAbsolute},
		/* 0x6E */ {"ROR", c.ror, 3, 6, 0, c.getAddrAbsolute},
		/* 0x6F */ {"RRA", c.rra, 3, 6, 0, c.getAddrAbsolute},
		/* 0x70 */ {"BVS", c.bvs, 2, 2, 0, c.getAddrRelative},
		/* 0x71 */ {"ADC", c.adc, 2, 5, 1, c.getAddrIndirectY},
		/* 0x72 */ {"x72", c.xxx, 0, 0, 0, nil},
		/* 0x73 */ {"RRA", c.rra, 2, 8, 0, c.getAddrIndirectY},
		/* 0x74 */ {"DOP", c.dop, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x75 */ {"ADC", c.adc, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x76 */ {"ROR", c.ror, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x77 */ {"RRA", c.rra, 2, 6, 0, c.getAddrZeroPageX},
		/* 0x78 */ {"SEI", c.sei, 1, 2, 0, c.getAddrImplied},
		/* 0x79 */ {"ADC", c.adc, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0x7A */ {"NOP", c.nop, 1, 2, 0, c.getAddrImplied},
		/* 0x7B */ {"RRA", c.rra, 3, 7, 0, c.getAddrAbsoluteY},
		/* 0x7C */ {"TOP", c.top, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x7D */ {"ADC", c.adc, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0x7E */ {"ROR", c.ror, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x7F */ {"RRA", c.rra, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0x80 */ {"DOP", c.dop, 2, 2, 0, c.getAddrImmediate},
		/* 0x81 */ {"STA", c.sta, 2, 6, 0, c.getAddrIndirectX},
		/* 0x82 */ {"DOP", c.dop, 2, 2, 0, c.getAddrImmediate},
		/* 0x83 */ {"AAX", c.aax, 2, 6, 0, c.getAddrIndirectX},
		/* 0x84 */ {"STY", c.sty, 2, 3, 0, c.getAddrZeroPage},
		/* 0x85 */ {"STA", c.sta, 2, 3, 0, c.getAddrZeroPage},
		/* 0x86 */ {"STX", c.stx, 2, 3, 0, c.getAddrZeroPage},
		/* 0x87 */ {"AAX", c.aax, 2, 3, 0, c.getAddrZeroPage},
		/* 0x88 */ {"DEY", c.dey, 1, 2, 0, c.getAddrImplied},
		/* 0x89 */ {"DOP", c.dop, 2, 2, 0, c.getAddrImmediate},
		/* 0x8A */ {"TXA", c.txa, 1, 2, 0, c.getAddrImplied},
		/* 0x8B */ {"x8B", c.xxx, 0, 0, 0, nil},
		/* 0x8C */ {"STY", c.sty, 3, 4, 0, c.getAddrAbsolute},
		/* 0x8D */ {"STA", c.sta, 3, 4, 0, c.getAddrAbsolute},
		/* 0x8E */ {"STX", c.stx, 3, 4, 0, c.getAddrAbsolute},
		/* 0x8F */ {"AAX", c.aax, 3, 4, 0, c.getAddrAbsolute},
		/* 0x90 */ {"BCC", c.bcc, 2, 2, 0, c.getAddrRelative},
		/* 0x91 */ {"STA", c.sta, 2, 6, 0, c.getAddrIndirectY},
		/* 0x92 */ {"x92", c.xxx, 0, 0, 0, nil},
		/* 0x93 */ {"x93", c.xxx, 0, 0, 0, nil},
		/* 0x94 */ {"STY", c.sty, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x95 */ {"STA", c.sta, 2, 4, 0, c.getAddrZeroPageX},
		/* 0x96 */ {"STX", c.stx, 2, 4, 0, c.getAddrZeroPageY},
		/* 0x97 */ {"AAX", c.aax, 2, 4, 0, c.getAddrZeroPageY},
		/* 0x98 */ {"TYA", c.tya, 1, 2, 0, c.getAddrImplied},
		/* 0x99 */ {"STA", c.sta, 3, 5, 0, c.getAddrAbsoluteY},
		/* 0x9A */ {"TXS", c.txs, 1, 2, 0, c.getAddrImplied},
		/* 0x9B */ {"x9B", c.xxx, 0, 0, 0, nil},
		/* 0x9C */ {"x9C", c.xxx, 0, 0, 0, nil},
		/* 0x9D */ {"STA", c.sta, 3, 5, 0, c.getAddrAbsoluteX},
		/* 0x9E */ {"x9E", c.xxx, 0, 0, 0, nil},
		/* 0x9F */ {"x9F", c.xxx, 0, 0, 0, nil},
		/* 0xA0 */ {"LDY", c.ldy, 2, 2, 0, c.getAddrImmediate},
		/* 0xA1 */ {"LDA", c.lda, 2, 6, 0, c.getAddrIndirectX},
		/* 0xA2 */ {"LDX", c.ldx, 2, 2, 0, c.getAddrImmediate},
		/* 0xA3 */ {"LAX", c.lax, 2, 6, 0, c.getAddrIndirectX},
		/* 0xA4 */ {"LDY", c.ldy, 2, 3, 0, c.getAddrZeroPage},
		/* 0xA5 */ {"LDA", c.lda, 2, 3, 0, c.getAddrZeroPage},
		/* 0xA6 */ {"LDX", c.ldx, 2, 3, 0, c.getAddrZeroPage},
		/* 0xA7 */ {"LAX", c.lax, 2, 3, 0, c.getAddrZeroPage},
		/* 0xA8 */ {"TAY", c.tay, 1, 2, 0, c.getAddrImplied},
		/* 0xA9 */ {"LDA", c.lda, 2, 2, 0, c.getAddrImmediate},
		/* 0xAA */ {"TAX", c.tax, 1, 2, 0, c.getAddrImplied},
		/* 0xAB */ {"LXA", c.lxa, 2, 2, 0, c.getAddrImmediate},
		/* 0xAC */ {"LDY", c.ldy, 3, 4, 0, c.getAddrAbsolute},
		/* 0xAD */ {"LDA", c.lda, 3, 4, 0, c.getAddrAbsolute},
		/* 0xAE */ {"LDX", c.ldx, 3, 4, 0, c.getAddrAbsolute},
		/* 0xAF */ {"LAX", c.lax, 3, 4, 0, c.getAddrAbsolute},
		/* 0xB0 */ {"BCS", c.bcs, 2, 2, 0, c.getAddrRelative},
		/* 0xB1 */ {"LDA", c.lda, 2, 5, 1, c.getAddrIndirectY},
		/* 0xB2 */ {"xB2", c.xxx, 0, 0, 0, nil},
		/* 0xB3 */ {"LAX", c.lax, 2, 5, 1, c.getAddrIndirectY},
		/* 0xB4 */ {"LDY", c.ldy, 2, 4, 0, c.getAddrZeroPageX},
		/* 0xB5 */ {"LDA", c.lda, 2, 4, 0, c.getAddrZeroPageX},
		/* 0xB6 */ {"LDX", c.ldx, 2, 4, 0, c.getAddrZeroPageY},
		/* 0xB7 */ {"LAX", c.lax, 2, 4, 0, c.getAddrZeroPageY},
		/* 0xB8 */ {"CLV", c.clv, 1, 2, 0, c.getAddrImplied},
		/* 0xB9 */ {"LDA", c.lda, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0xBA */ {"TSX", c.tsx, 1, 2, 0, c.getAddrImplied},
		/* 0xBB */ {"xBB", c.xxx, 0, 0, 0, nil},
		/* 0xBC */ {"LDY", c.ldy, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0xBD */ {"LDA", c.lda, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0xBE */ {"LDX", c.ldx, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0xBF */ {"LAX", c.lax, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0xC0 */ {"CPY", c.cpy, 2, 2, 0, c.getAddrImmediate},
		/* 0xC1 */ {"CMP", c.cmp, 2, 6, 0, c.getAddrIndirectX},
		/* 0xC2 */ {"DOP", c.dop, 2, 2, 0, c.getAddrImmediate},
		/* 0xC3 */ {"DCP", c.dcp, 2, 8, 0, c.getAddrIndirectX},
		/* 0xC4 */ {"CPY", c.cpy, 2, 3, 0, c.getAddrZeroPage},
		/* 0xC5 */ {"CMP", c.cmp, 2, 3, 0, c.getAddrZeroPage},
		/* 0xC6 */ {"DEC", c.dec, 2, 5, 0, c.getAddrZeroPage},
		/* 0xC7 */ {"DCP", c.dcp, 2, 5, 0, c.getAddrZeroPage},
		/* 0xC8 */ {"INY", c.iny, 1, 2, 0, c.getAddrImplied},
		/* 0xC9 */ {"CMP", c.cmp, 2, 2, 0, c.getAddrImmediate},
		/* 0xCA */ {"DEX", c.dex, 1, 2, 0, c.getAddrImplied},
		/* 0xCB */ {"SAX", c.sax, 2, 2, 0, c.getAddrImmediate},
		/* 0xCC */ {"CPY", c.cpy, 3, 4, 0, c.getAddrAbsolute},
		/* 0xCD */ {"CMP", c.cmp, 3, 4, 0, c.getAddrAbsolute},
		/* 0xCE */ {"DEC", c.dec, 3, 6, 0, c.getAddrAbsolute},
		/* 0xCF */ {"DCP", c.dcp, 3, 6, 0, c.getAddrAbsolute},
		/* 0xD0 */ {"BNE", c.bne, 2, 2, 0, c.getAddrRelative},
		/* 0xD1 */ {"CMP", c.cmp, 2, 5, 1, c.getAddrIndirectY},
		/* 0xD2 */ {"xD2", c.xxx, 0, 0, 0, nil},
		/* 0xD3 */ {"DCP", c.dcp, 2, 8, 0, c.getAddrIndirectY},
		/* 0xD4 */ {"DOP", c.dop, 2, 4, 0, c.getAddrZeroPageX},
		/* 0xD5 */ {"CMP", c.cmp, 2, 4, 0, c.getAddrZeroPageX},
		/* 0xD6 */ {"DEC", c.dec, 2, 6, 0, c.getAddrZeroPageX},
		/* 0xD7 */ {"DCP", c.dcp, 2, 6, 0, c.getAddrZeroPageX},
		/* 0xD8 */ {"CLD", c.cld, 1, 2, 0, c.getAddrImplied},
		/* 0xD9 */ {"CMP", c.cmp, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0xDA */ {"NOP", c.nop, 1, 2, 0, c.getAddrImplied},
		/* 0xDB */ {"DCP", c.dcp, 3, 7, 0, c.getAddrAbsoluteY},
		/* 0xDC */ {"TOP", c.top, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0xDD */ {"CMP", c.cmp, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0xDE */ {"DEC", c.dec, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0xDF */ {"DCP", c.dcp, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0xE0 */ {"CPX", c.cpx, 2, 2, 0, c.getAddrImmediate},
		/* 0xE1 */ {"SBC", c.sbc, 2, 6, 0, c.getAddrIndirectX},
		/* 0xE2 */ {"DOP", c.dop, 2, 2, 0, c.getAddrImmediate},
		/* 0xE3 */ {"ISC", c.isc, 2, 8, 0, c.getAddrIndirectX},
		/* 0xE4 */ {"CPX", c.cpx, 2, 3, 0, c.getAddrZeroPage},
		/* 0xE5 */ {"SBC", c.sbc, 2, 3, 0, c.getAddrZeroPage},
		/* 0xE6 */ {"INC", c.inc, 2, 5, 0, c.getAddrZeroPage},
		/* 0xE7 */ {"ISC", c.isc, 2, 5, 0, c.getAddrZeroPage},
		/* 0xE8 */ {"INX", c.inx, 1, 2, 0, c.getAddrImplied},
		/* 0xE9 */ {"SBC", c.sbc, 2, 2, 0, c.getAddrImmediate},
		/* 0xEA */ {"NOP", c.nop, 1, 2, 0, c.getAddrImplied},
		/* 0xEB */ {"SBC", c.sbc, 2, 2, 0, c.getAddrImmediate},
		/* 0xEC */ {"CPX", c.cpx, 3, 4, 0, c.getAddrAbsolute},
		/* 0xED */ {"SBC", c.sbc, 3, 4, 0, c.getAddrAbsolute},
		/* 0xEE */ {"INC", c.inc, 3, 6, 0, c.getAddrAbsolute},
		/* 0xEF */ {"ISC", c.isc, 3, 6, 0, c.getAddrAbsolute},
		/* 0xF0 */ {"BEQ", c.beq, 2, 2, 0, c.getAddrRelative},
		/* 0xF1 */ {"SBC", c.sbc, 2, 5, 1, c.getAddrIndirectY},
		/* 0xF2 */ {"xF2", c.xxx, 0, 0, 0, nil},
		/* 0xF3 */ {"ISC", c.isc, 2, 8, 0, c.getAddrIndirectY},
		/* 0xF4 */ {"DOP", c.dop, 2, 4, 0, c.getAddrZeroPageX},
		/* 0xF5 */ {"SBC", c.sbc, 2, 4, 0, c.getAddrZeroPageX},
		/* 0xF6 */ {"INC", c.inc, 2, 6, 0, c.getAddrZeroPageX},
		/* 0xF7 */ {"ISC", c.isc, 2, 6, 0, c.getAddrZeroPageX},
		/* 0xF8 */ {"SED", c.sed, 1, 2, 0, c.getAddrImplied},
		/* 0xF9 */ {"SBC", c.sbc, 3, 4, 1, c.getAddrAbsoluteY},
		/* 0xFA */ {"NOP", c.nop, 1, 2, 0, c.getAddrImplied},
		/* 0xFB */ {"ISC", c.isc, 3, 7, 0, c.getAddrAbsoluteY},
		/* 0xFC */ {"TOP", c.top, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0xFD */ {"SBC", c.sbc, 3, 4, 1, c.getAddrAbsoluteX},
		/* 0xFE */ {"INC", c.inc, 3, 7, 0, c.getAddrAbsoluteX},
		/* 0xFF */ {"ISC", c.isc, 3, 7, 0, c.getAddrAbsoluteX},
	}
}
