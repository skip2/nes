package nes

import (
	"image"
	"time"
)

// Number of video frames per second.
const framesPerSecond = 60

// Console represents a NES console and its main hardware components (the
// cartridge, CPU, PPU, and joypads).
type Console struct {
	Cart    *Cartridge
	CPU     *CPU
	PPU     *PPU
	Joypads [2]*Joypad

	lastFrameStart time.Time
	frameDuration  time.Duration
	frameCount     uint64
}

// NewConsole returns a Console initialised with cart.
func NewConsole(cart *Cartridge) *Console {
	c := &Console{}
	c.Cart = cart
	c.CPU = NewCPU(c)
	c.PPU = NewPPU(c)

	for i := range c.Joypads {
		c.Joypads[i] = NewJoypad()
	}

	c.lastFrameStart = time.Now()
	c.frameDuration = time.Second / framesPerSecond

	return c
}

// Step runs the Console for 1 CPU instruction. The PPU runs at the same time.
//
// Call Step() repeatedly to simulate the Console. For the majority of calls,
// Step() returns a nil *image.RGBA. Approximately 60 times a second the PPU
// emits a new image frame, and a non-nil *image.RGBA is returned. The image is
// 256x240px.
//
// To regulate emulation speed, Step() may sleep when emitting an image. It
// sleeps to regulate the output to around 60 frames per second (as per NTSC).
func (c *Console) Step() (*image.RGBA, error) {
	var cpuCycles uint64
	var ppuCycles uint64

	cpuCycles, err := c.CPU.Step()
	if err != nil {
		return nil, err
	}

	for ppuCycles < cpuCycles*3 {
		var image *image.RGBA
		ppuCycles, image = c.PPU.Step()

		if image != nil {
			c.frameCount++

			// Regulate frames per second.
			expectedTime := c.lastFrameStart.Add(c.frameDuration)
			actualTime := time.Now()
			sleepDuration := expectedTime.Sub(actualTime)

			time.Sleep(sleepDuration)

			c.lastFrameStart = time.Now()

			return image, nil
		}
	}

	return nil, nil
}
