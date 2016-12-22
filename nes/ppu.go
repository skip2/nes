package nes

import (
	"fmt"
	"image"
	"image/color"
)

// PPU implements the NES Picture Processing Unit.
type PPU struct {
	Console *Console

	// Screen image, 256x240px.
	img *image.RGBA

	// NES fixed 64 colour palette.
	palette [64]color.RGBA

	// Scanline (0-261).
	Scanline int

	// Tick (0-340).
	Tick int

	// Frame counter.
	Frame uint64

	// Total number of cycles executed.
	numCycles uint64

	// RAM.
	ram [16384]byte

	// Sprite RAM.
	sprRAM [256]byte

	// PPU Control Register 1 ($2000).
	spriteTableAddress     uint16
	backgroundTableAddress uint16
	flagIncrementBy32      bool
	flagLargeSprites       bool
	flagNMIOnVBlank        bool

	// PPU Control Register 2 ($2001).
	flagColourMode     bool
	flagClipBackground bool
	flagClipSprites    bool
	flagShowBackground bool
	flagShowSprites    bool
	flagRedEmphasis    bool
	flagGreenEmphasis  bool
	flagBlueEmphasis   bool

	// Misc flags.
	flagVRAMWritesIgnored  bool
	flagScanlineSpritesMax bool
	flagSprite0Hit         bool
	flagVBlankOutstanding  bool

	// Internal registers.
	v uint16 // Current VRAM address (15 bits).
	t uint16 // Temporary VRAM address. (15 bits).
	x byte   // Fine X scroll (3 bits).
	w byte   // First or second write toggle (0=first, 1=second).

	// A complete scanline of foreground pixels (i.e. sprites).
	fgPixels         [256]*color.RGBA
	fgPixelIsSprite0 [256]bool
	fgPixelIsInFront [256]bool

	// The next 16 pixels of background.
	bgPixels [16]*color.RGBA

	// Sprite IO address.
	sprIOAddress byte

	// PPUDATA read buffer.
	readBuffer byte
}

const BackgroundPaletteAddress = 0x3F00
const SpritePaletteAddress = 0x3F10

// NewPPU constructs and returns a PPU for the given console.
func NewPPU(console *Console) *PPU {
	p := &PPU{
		Console:  console,
		Scanline: 241,
		Tick:     0,
		img:      image.NewRGBA(image.Rect(0, 0, 256, 240))}

	p.setupPalette()
	p.flagShowBackground = true

	return p
}

// Step runs the PPU for one cycle.
//
// Returns the total number of cycles run in the PPU's lifetime. An image is
// returned when the screen is to be updated.
func (p *PPU) Step() (uint64, *image.RGBA) {
	p.incrementTick()

	var outputImage *image.RGBA = nil

	// True if rendering is enabled.
	var isRendering bool = p.flagShowBackground || p.flagShowSprites

	// True if this is a visible scanline.
	var isVisible = p.Scanline <= 239

	// True if this is the interrupt assert scanline.
	var isVBlankLine bool = p.Scanline == 241

	// True if this is the prerender scanline.
	var isPrerender bool = p.Scanline == 261

	// True if a pixel should be drawn this tick.
	var isDrawing bool = isRendering && isVisible &&
		((p.Tick >= 1 && p.Tick <= 256) || (p.Tick >= 321 && p.Tick <= 336))

	// True if a background tile should be fetched this tick.
	var isFetching bool = isRendering && (isVisible || isPrerender) &&
		((p.Tick >= 1 && p.Tick <= 256) || (p.Tick >= 321 && p.Tick <= 336)) &&
		p.Tick%8 == 0

	// Draw pixels.
	if isDrawing {
		p.drawPixel()
	}

	// Fetch background tiles.
	if isFetching {
		p.loadTile()

		if p.Tick == 256 {
			// Horizontal bits are set on tick 257.
			p.incrementY()
		} else {
			p.incrementCoarseX()
		}
	}

	// Interrupt generation.
	if isVBlankLine && p.Tick == 1 {
		// Generate interrupt.
		p.flagVBlankOutstanding = true
		if p.flagNMIOnVBlank {
			p.Console.CPU.NMI()
		}
		outputImage = p.img
	} else if isPrerender && p.Tick == 1 {
		// Clear flags.
		p.flagVBlankOutstanding = false
		p.flagScanlineSpritesMax = false
		p.flagSprite0Hit = false
	}

	// Load sprites.
	if isRendering && p.Tick == 257 {
		p.loadSprites()
	}

	// For scanline counting mappers.
	if (isVisible || isPrerender) && (p.flagShowBackground || p.flagShowSprites) && p.Tick == 260 {
		p.Console.Cart.NextScanline()
	}

	// Update v from t.
	if isRendering {
		if (isVisible || isPrerender) && p.Tick == 257 {
			p.copyHorizontalBitsToV()
		} else if isPrerender && p.Tick == 304 {
			p.copyVerticalBitsToV()
		}
	}

	p.numCycles++
	return p.numCycles, outputImage
}

func (p *PPU) loadTile() {
	// Load the tile's attribute bits.
	var attributeAddress uint16 = 0x23C0 | (p.v & 0x0C00) | ((p.v >> 4) & 0x38) |
		((p.v >> 2) & 0x07)
	var shift uint16 = p.v&0x2 | ((p.v & 0x40) >> 4)
	var attributeBits byte = (p.read(attributeAddress) >> shift) & 0x3

	// Load the tile's pattern index.
	var patternIndex byte = p.read(0x2000 | (p.v & 0x0FFF))

	// Build 8 pixel strip of the tile.
	var newPixels [8]*color.RGBA = p.pixelStrip(patternIndex, uint16(attributeBits),
		false, int(p.v&0x7000)>>12)

	// Add pixels to the bgPixels shift register.
	copy(p.bgPixels[8:], newPixels[:])
}

func (p *PPU) drawPixel() {
	// Select background pixel, move up remaining pixels in shift register.
	var bgPixel *color.RGBA = p.bgPixels[p.x]

	// Move the shift register along.
	copy(p.bgPixels[p.x:], p.bgPixels[p.x+1:])

	// X coordinate (0-255).
	var x int = p.Tick - 1

	// Nothing to render here.
	if x > 256 {
		return
	}

	// Get the foreground pixel (if any), choose the final pixel to render.
	var colour color.RGBA

	// Clipping.
	var showSprites bool = x >= 8 || !p.flagClipSprites
	var showBackground bool = x >= 8 || !p.flagClipBackground
	var isBorder bool = x < 8 || x > 247 || p.Scanline < 8 || p.Scanline > 231

	if isBorder {
		colour = p.palette[0x3F] // black
	} else if showSprites && p.fgPixels[x] != nil && (p.fgPixelIsInFront[x] || bgPixel == nil) {
		colour = *p.fgPixels[x]
	} else if showBackground && bgPixel != nil {
		colour = *bgPixel
	} else {
		colour = p.palette[p.read(BackgroundPaletteAddress) & 0x3F]
	}

	// Sprite 0 hit?
	if showSprites && showBackground {
		if p.fgPixels[x] != nil && bgPixel != nil && p.fgPixelIsSprite0[x] && x < 255 {
			p.flagSprite0Hit = true
		}
	}

	p.img.Set(x, p.Scanline, colour)
}

// String returns a description of the PPU as a string.
func (p *PPU) String() string {
	return fmt.Sprintf("PPU[Scanline=%d, Tick=%d, CX=%d, FX=%d, CY=%d, FY=%d, NT=%d, R=%v]",
		p.Scanline,
		p.Tick,
		p.v&0x001F,
		p.x,
		(p.v&0x03E0)>>5,
		(p.v&0x7000)>>12,
		(p.v&0x0800)>>11,
		p.flagShowBackground || p.flagShowSprites)
}

func (p *PPU) incrementCoarseX() {
	// If coarse X = 31...
	if (p.v & 0x001F) == 31 {
		// Coarse X = 0, switch nametable.
		p.v &^= 0x001F
		p.v ^= 0x0400
	} else {
		// Otherwise increment coarse X.
		p.v++
	}
}

func (p *PPU) incrementY() {
	// If fine Y < 7...
	if (p.v & 0x7000) != 0x7000 {
		// Increment fine Y.
		p.v += 0x1000
	} else {
		// Fine Y = 0
		p.v &^= 0x7000

		// Extract coarse Y.
		y := (p.v & 0x03E0) >> 5

		if y == 29 {
			// Last scanline in frame, so set y=0, switch nametable.
			y = 0
			p.v ^= 0x0800
		} else if y == 31 {
			// Out of bounds Y, just set y=0.
			y = 0
		} else {
			y++
		}

		// Reinsert coarse Y.
		p.v = (p.v &^ 0x03E0) | (y << 5)
	}
}

func (p *PPU) copyHorizontalBitsToV() {
	// v: ....F.. ...EDCBA = t: ....F.. ...EDCBA
	p.v = (p.v & 0xFBE0) | (p.t &^ 0xFBE0)
}

func (p *PPU) copyVerticalBitsToV() {
	// v: IHGF.ED CBA..... = t: IHGF.ED CBA.....
	p.v = (p.v & 0x041F) | (p.t &^ 0x041F)
}

func (p *PPU) incrementTick() {
	p.Tick++

	isOddFrame := p.Frame&0x1 != 0

	if p.Scanline == 261 && (p.Tick == 341 || (p.Tick == 340 && isOddFrame)) {
		p.Scanline = 0
		p.Tick = 0
		p.Frame++
	} else if p.Tick == 341 {
		p.Scanline++
		p.Tick = 0
	}
}

// SetControlRegister sets the value of the control register ($2000).
func (p *PPU) SetControlRegister(value byte) {
	// t: ...BA.. ........ = d: ......BA
	p.t = p.t&0x73FF | (uint16(value)&0x3)<<10

	p.flagIncrementBy32 = value&0x4 != 0

	if value&0x8 == 0 {
		p.spriteTableAddress = 0x0000
	} else {
		p.spriteTableAddress = 0x1000
	}

	if value&0x10 == 0 {
		p.backgroundTableAddress = 0x0000
	} else {
		p.backgroundTableAddress = 0x1000
	}

	p.flagLargeSprites = value&0x20 != 0
	p.flagNMIOnVBlank = value&0x80 != 0
}

// SetMaskRegister sets the value of the mask register ($2001).
func (p *PPU) SetMaskRegister(value byte) {
	p.flagColourMode = value&0x1 == 0

	p.flagClipBackground = value&0x2 == 0
	p.flagClipSprites = value&0x4 == 0

	p.flagShowBackground = value&0x8 != 0
	p.flagShowSprites = value&0x10 != 0

	p.flagRedEmphasis = value&0x20 != 0
	p.flagGreenEmphasis = value&0x40 != 0
	p.flagBlueEmphasis = value&0x80 != 0
}

// SetSPRAddress sets the value of the sprite address register ($2003).
func (p *PPU) SetSPRAddress(address byte) {
	p.sprIOAddress = address
}

// WriteSPR writes a byte to the sprite memory at the current sprite address.
// The sprite address is then incremented.
func (p *PPU) WriteSPR(value byte) {
	p.sprRAM[p.sprIOAddress] = value
	p.sprIOAddress++
}

// ReadSPR reads and returns the byte at the current sprite address.
func (p *PPU) ReadSPR() byte {
	return p.sprRAM[p.sprIOAddress]
}

// WriteScroll ($2005): Scroll position write register (two values).
func (p *PPU) WriteScroll(value byte) {
	if p.w == 0 {
		// t: ....... ...HGFED = d: HGFED...
		// x:              CBA = d: .....CBA
		// w:                  = 1
		p.t = (p.t & 0xFFE0) | ((uint16(value) & 0xF8) >> 3)
		p.x = value & 0x7
		p.w = 1

	} else {
		// t: CBA..HG FED..... = d: HGFEDCBA
		// w:                  = 0
		p.t = (p.t & 0x0C1F) | ((uint16(value) & 0x7) << 12) |
			((uint16(value) & 0xF8) << 2)
		p.w = 0
	}
}

// WriteDataAddress ($2006): Data address write register (two values).
func (p *PPU) WriteDataAddress(value byte) {
	if p.w == 0 {
		// t: .FEDCBA ........ = d: ..FEDCBA
		// t: X...... ........ = 0
		// w:                  = 1
		p.t = (p.t & 0x00FF) | ((uint16(value) & 0x3F) << 8)
		p.t &= 0x7FFF
		p.w = 1
	} else {
		// t: ....... HGFEDCBA = d: HGFEDCBA
		// v                   = t
		// w:                  = 0
		p.t = (p.t & 0xFF00) | uint16(value)
		p.v = p.t
		p.w = 0
	}
}

// WriteData writes a byte to PPU RAM ($2007).
func (p *PPU) WriteData(value byte) {
	p.write(p.v, value)

	if p.flagIncrementBy32 {
		p.v += 32
	} else {
		p.v += 1
	}
}

// ReadData reads and returns a byte from PPU RAM. ($2007).
func (p *PPU) ReadData() byte {
	previousValue := p.readBuffer
	p.readBuffer = p.read(p.v)

	var result byte

	if p.v&0x3FFF <= 0x3EFF {
		result = previousValue
	} else {
		result = p.readBuffer
	}

	if p.flagIncrementBy32 {
		p.v += 32
	} else {
		p.v += 1
	}

	return result
}

// StatusRegister returns the value of the status register ($2002).
func (p *PPU) StatusRegister() byte {
	var result byte

	if p.flagScanlineSpritesMax {
		result |= 0x20
	}

	if p.flagSprite0Hit {
		result |= 0x40
	}

	if p.flagVBlankOutstanding {
		result |= 0x80
		p.flagVBlankOutstanding = false
	}

	// w:                  = 0
	p.w = 0

	return result
}

func (p *PPU) loadSprites() {
	for i := range p.fgPixels {
		p.fgPixels[i] = nil
		p.fgPixelIsSprite0[i] = false
		p.fgPixelIsInFront[i] = false
	}

	if !p.flagShowSprites || p.Scanline == 0 {
		return
	}

	spriteHeight := 8
	if p.flagLargeSprites {
		spriteHeight = 16
	}

	numSprites := 0
	for i := 0; i < len(p.sprRAM); i += 4 {
		y := int(p.sprRAM[i]) + 1
		x := int(p.sprRAM[i+3])

		if y >= 0xF0 || p.Scanline+1 < y || p.Scanline+1 >= y+spriteHeight {
			continue
		}

		numSprites++
		if numSprites > 8 {
			p.flagScanlineSpritesMax = true
			break
		}

		yOffset := p.Scanline + 1 - y
		patternIndex := p.sprRAM[i+1]

		attributes := p.sprRAM[i+2]
		flipH := attributes&0x40 != 0
		flipV := attributes&0x80 != 0
		inFront := attributes&0x20 == 0

		if flipV {
			yOffset = spriteHeight - 1 - yOffset
		}

		paletteBits := uint16(attributes & 0x3)
		var fgPixels [8]*color.RGBA = p.pixelStrip(patternIndex, paletteBits, true, yOffset)

		for k := 0; k < 8; k++ {
			pk := k
			if flipH {
				pk = 7 - k
			}

			if x+k > 0xFF {
				break
			}

			pos := x + k
			if p.fgPixels[pos] == nil && fgPixels[pk] != nil {
				p.fgPixels[pos] = fgPixels[pk]

				if i == 0 {
					p.fgPixelIsSprite0[pos] = true
				}

				p.fgPixelIsInFront[pos] = inFront
			}
		}
	}
}

func (p *PPU) pixelStrip(patternIndex byte, attributeBits uint16, isForeground bool, yOffset int) [8]*color.RGBA {
	var baseAddress uint16
	var basePaletteAddress uint16
	var showPixels bool

	if isForeground {
		if p.flagLargeSprites {
			if patternIndex&0x1 == 0 {
				baseAddress = 0x0000
			} else {
				baseAddress = 0x1000
			}

			if yOffset > 7 {
				patternIndex |= 0x1
				yOffset -= 8
			} else {
				patternIndex &^= 0x1
			}
		} else {
			baseAddress = p.spriteTableAddress
		}
		basePaletteAddress = SpritePaletteAddress
		showPixels = p.flagShowSprites
	} else {
		baseAddress = p.backgroundTableAddress
		basePaletteAddress = BackgroundPaletteAddress
		showPixels = p.flagShowBackground
	}

	var result [8]*color.RGBA

	low := p.read(baseAddress + uint16(patternIndex)*16 + uint16(yOffset))
	high := p.read(baseAddress + uint16(patternIndex)*16 + uint16(yOffset) + 8)

	for i := 0; i < 8; i++ {
		low2 := (low>>uint(7-i))&0x1 != 0
		high2 := (high>>uint(7-i))&0x1 != 0

		var index uint16
		if high2 {
			index |= 0x2
		}
		if low2 {
			index |= 0x1
		}

		if index == 0 || !showPixels {
			result[i] = nil
		} else {
			palette_index := p.read(basePaletteAddress+attributeBits<<2+index) & 0x3F;
			result[i] = &p.palette[palette_index]
		}
	}

	return result
}

func (p *PPU) mapAddress(address uint16) uint16 {
	address &= 0x3FFF

	// Sprite palette mirroring.
	if address == 0x3F10 ||
		address == 0x3F14 ||
		address == 0x3F18 ||
		address == 0x3F1C {
		address -= 0x10
	} else if address >= 0x2000 && address <= 0x2FFF {
		// Nametable mirroring.
		mirror := p.Console.Cart.Mirror

		if mirror == horizontal {
			if address >= 0x2400 && address < 0x2800 {
				address -= 0x400
			} else if address >= 0x2C00 && address < 0x3000 {
				address -= 0x400
			}
		} else if mirror == vertical {
			if address >= 0x2800 && address < 0x2C00 {
				address -= 0x800
			} else if address >= 0x2C00 && address < 0x3000 {
				address -= 0x800
			}
		} else if mirror == singleLow {
			address = 0x2000 | (address & 0x3FF)
		} else if mirror == singleHigh {
			address = 0x2400 | (address & 0x3FF)
			//log.Printf("address %x mirrored to %x\n", orig, address)
		} else if mirror == fourScreen {
			// No mirroring.
		}
	}

	return address
}

func (p *PPU) read(address uint16) byte {
	address = p.mapAddress(address)

	var result byte

	switch {
	case address < 0x2000:
		result = p.Console.Cart.Read(address, true)
	default:
		result = p.ram[address]
	}

	return result
}

func (p *PPU) write(address uint16, value byte) {
	address = p.mapAddress(address)
	
	switch {
	case address < 0x2000:
		p.Console.Cart.Write(address, value, true)
	default:
		p.ram[address] = value
	}
}

func (p *PPU) setupPalette() {
	p.palette = [64]color.RGBA{
		/* 0x00 */ {0x75, 0x75, 0x75, 0xFF},
		/* 0x01 */ {0x27, 0x1B, 0x8F, 0xFF},
		/* 0x02 */ {0x00, 0x00, 0xAB, 0xFF},
		/* 0x03 */ {0x47, 0x00, 0x9F, 0xFF},
		/* 0x04 */ {0x8F, 0x00, 0x77, 0xFF},
		/* 0x05 */ {0xAB, 0x00, 0x13, 0xFF},
		/* 0x06 */ {0xA7, 0x00, 0x00, 0xFF},
		/* 0x07 */ {0x7F, 0x0B, 0x00, 0xFF},
		/* 0x08 */ {0x43, 0x2F, 0x00, 0xFF},
		/* 0x09 */ {0x00, 0x47, 0x00, 0xFF},
		/* 0x0A */ {0x00, 0x51, 0x00, 0xFF},
		/* 0x0B */ {0x00, 0x3F, 0x17, 0xFF},
		/* 0x0C */ {0x1B, 0x3F, 0x5F, 0xFF},
		/* 0x0D */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x0E */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x0F */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x10 */ {0xBC, 0xBC, 0xBC, 0xFF},
		/* 0x11 */ {0x00, 0x73, 0xEF, 0xFF},
		/* 0x12 */ {0x23, 0x3B, 0xEF, 0xFF},
		/* 0x13 */ {0x83, 0x00, 0xF3, 0xFF},
		/* 0x14 */ {0xBF, 0x00, 0xBF, 0xFF},
		/* 0x15 */ {0xE7, 0x00, 0x5B, 0xFF},
		/* 0x16 */ {0xDB, 0x2B, 0x00, 0xFF},
		/* 0x17 */ {0xCB, 0x4F, 0x0F, 0xFF},
		/* 0x18 */ {0x8B, 0x73, 0x00, 0xFF},
		/* 0x19 */ {0x00, 0x97, 0x00, 0xFF},
		/* 0x1A */ {0x00, 0xAB, 0x00, 0xFF},
		/* 0x1B */ {0x00, 0x93, 0x3B, 0xFF},
		/* 0x1C */ {0x00, 0x83, 0x8B, 0xFF},
		/* 0x1D */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x1E */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x1F */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x20 */ {0xFF, 0xFF, 0xFF, 0xFF},
		/* 0x21 */ {0x3F, 0xBF, 0xFF, 0xFF},
		/* 0x22 */ {0x5F, 0x97, 0xFF, 0xFF},
		/* 0x23 */ {0xA7, 0x8B, 0xFD, 0xFF},
		/* 0x24 */ {0xF7, 0x7B, 0xFF, 0xFF},
		/* 0x25 */ {0xFF, 0x77, 0xB7, 0xFF},
		/* 0x26 */ {0xFF, 0x77, 0x63, 0xFF},
		/* 0x27 */ {0xFF, 0x9B, 0x3B, 0xFF},
		/* 0x28 */ {0xF3, 0xBF, 0x3F, 0xFF},
		/* 0x29 */ {0x83, 0xD3, 0x13, 0xFF},
		/* 0x2A */ {0x4F, 0xDF, 0x4B, 0xFF},
		/* 0x2B */ {0x58, 0xF8, 0x98, 0xFF},
		/* 0x2C */ {0x00, 0xEB, 0xDB, 0xFF},
		/* 0x2D */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x2E */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x2F */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x30 */ {0xFF, 0xFF, 0xFF, 0xFF},
		/* 0x31 */ {0xAB, 0xE7, 0xFF, 0xFF},
		/* 0x32 */ {0xC7, 0xD7, 0xFF, 0xFF},
		/* 0x33 */ {0xD7, 0xCB, 0xFF, 0xFF},
		/* 0x34 */ {0xFF, 0xC7, 0xFF, 0xFF},
		/* 0x35 */ {0xFF, 0xC7, 0xDB, 0xFF},
		/* 0x36 */ {0xFF, 0xBF, 0xB3, 0xFF},
		/* 0x37 */ {0xFF, 0xDB, 0xAB, 0xFF},
		/* 0x38 */ {0xFF, 0xE7, 0xA3, 0xFF},
		/* 0x39 */ {0xE3, 0xFF, 0xA3, 0xFF},
		/* 0x3A */ {0xAB, 0xF3, 0xBF, 0xFF},
		/* 0x3B */ {0xB3, 0xFF, 0xCF, 0xFF},
		/* 0x3C */ {0x9F, 0xFF, 0xF3, 0xFF},
		/* 0x3D */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x3E */ {0x00, 0x00, 0x00, 0xFF},
		/* 0x3F */ {0x00, 0x00, 0x00, 0xFF},
	}
}
