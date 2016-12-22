package nes

import (
	"image"
	"image/png"
	"os"
	"runtime"

	"github.com/go-gl/gl/v2.1/gl"
	"github.com/go-gl/glfw/v3.1/glfw"
)

// Window dimensions.
const windowWidth = 256
const windowHeight = 240

type GUI struct {
	console *Console
	window  *glfw.Window
}

// NewGUI returns using the given console.
func NewGUI(console *Console) *GUI {
	return &GUI{console: console}
}

func init() {
	runtime.LockOSThread()
}

// Run opens a small 256x240px GUI window and runs the console.
//
// Input is via the arrow keys, enter, space, Z, X. Pressing S saves a
// screenshot to "screenshot.png".
//
// The function terminates when the Q key is pressed, or an error occurs.
func (g *GUI) Run() error {
	err := glfw.Init()
	if err != nil {
		return err
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.Resizable, glfw.False)

	g.window, err = glfw.CreateWindow(windowWidth, windowHeight, "NES emulator", nil, nil)
	if err != nil {
		return err
	}

	g.window.MakeContextCurrent()

	if err := gl.Init(); err != nil {
		return err
	}

	var console *Console = g.console
	console.Joypads[0].SetReadKeysCallback(func() {
		console.Joypads[0].A = g.isKeyPressed(glfw.KeyZ)
		console.Joypads[0].B = g.isKeyPressed(glfw.KeyX)
		console.Joypads[0].Select = g.isKeyPressed(glfw.KeySpace)
		console.Joypads[0].Start = g.isKeyPressed(glfw.KeyEnter)
		console.Joypads[0].Up = g.isKeyPressed(glfw.KeyUp)
		console.Joypads[0].Down = g.isKeyPressed(glfw.KeyDown)
		console.Joypads[0].Left = g.isKeyPressed(glfw.KeyLeft)
		console.Joypads[0].Right = g.isKeyPressed(glfw.KeyRight)
	})

	gl.ClearColor(0.0, 0.0, 0.0, 0.0)

	gl.MatrixMode(gl.PROJECTION)
	gl.LoadIdentity()

	gl.MatrixMode(gl.MODELVIEW)
	gl.LoadIdentity()

	for !g.window.ShouldClose() {
		image, err := console.Step()
		if err != nil {
			return err
		}

		if image != nil {
			g.doRedraw(image)
			glfw.PollEvents()

			if g.isKeyPressed(glfw.KeyS) {
				err = g.saveScreenshot(image)
				if err != nil {
					return err
				}
			} else if g.isKeyPressed(glfw.KeyQ) {
				break
			}
		}
	}

	return nil
}

// Saves the image as "screenshot.png".
func (g *GUI) saveScreenshot(image *image.RGBA) error {
	filename := "screenshot.png"

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	err = png.Encode(file, image)
	if err != nil {
		return err
	}

	return nil
}

// Redraws the screen with the image rgba.
//
// https://github.com/go-gl/examples/blob/master/glfw31-gl21-cube/cube.go
func (g *GUI) doRedraw(rgba *image.RGBA) {
	var texture uint32

	gl.Enable(gl.TEXTURE_2D)
	gl.GenTextures(1, &texture)
	gl.BindTexture(gl.TEXTURE_2D, texture)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexImage2D(
		gl.TEXTURE_2D,
		0,
		gl.RGBA,
		int32(rgba.Rect.Size().X),
		int32(rgba.Rect.Size().Y),
		0,
		gl.RGBA,
		gl.UNSIGNED_BYTE,
		gl.Ptr(rgba.Pix))

	gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

	gl.MatrixMode(gl.MODELVIEW)
	gl.LoadIdentity()

	gl.BindTexture(gl.TEXTURE_2D, texture)

	gl.Begin(gl.QUADS)

	gl.TexCoord2f(0, 1)
	gl.Vertex2f(-1, -1)

	gl.TexCoord2f(1, 1)
	gl.Vertex2f(1, -1)

	gl.TexCoord2f(1, 0)
	gl.Vertex2f(1, 1)

	gl.TexCoord2f(0, 0)
	gl.Vertex2f(-1, 1)

	gl.End()

	gl.DeleteTextures(1, &texture)

	g.window.SwapBuffers()
}

// Returns true if the key is currently pressed.
func (g *GUI) isKeyPressed(key glfw.Key) bool {
	return g.window.GetKey(key) == glfw.Press
}
