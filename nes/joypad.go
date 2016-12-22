package nes

// Joypad represents the state of a standard game controller.
//
// A standard controller has the buttons: A, B, Select, Start, Up, Down, Left,
// and Right.
//
// http://wiki.nesdev.com/w/index.php/Standard_controller
type Joypad struct {
	A bool
	B bool

	Select bool
	Start  bool

	Up    bool
	Down  bool
	Left  bool
	Right bool

	readKeys func()

	i      int
	strobe bool
}

// NewJoypad returns a new Joypad.
//
// Joypad does not perform any IO. Call SetReadKeysCallback() to provide a
// function to update the Joypad's state from the physical input device
// (keyboard/USB controller/etc).
func NewJoypad() *Joypad {
	return &Joypad{}
}

// SetReadKeysCallback sets a callback function used to update the Joypad's
// button press state.
//
// The callback function should update j.{A,B,Select,Start,Up,Down,Left,Right}
// to match the physical input device.
func (j *Joypad) SetReadKeysCallback(callback func()) {
	j.readKeys = callback
}

// Read reads a byte from the joypad's output register.
func (j *Joypad) Read() byte {
	var pressed bool

	if j.strobe {
		pressed = j.A
	} else {
		switch j.i {
		case 0:
			pressed = j.A
		case 1:
			pressed = j.B
		case 2:
			pressed = j.Select
		case 3:
			pressed = j.Start
		case 4:
			pressed = j.Up
		case 5:
			pressed = j.Down
		case 6:
			pressed = j.Left
		case 7:
			pressed = j.Right
		default:
			// Ignored.
		}

		j.i++
		if j.i == 8 {
			j.i = 0
		}
	}

	if pressed {
		return 0x1
	} else {
		return 0x0
	}
}

// Write writes a byte to the joypad's input register.
//
// The ReadKeysCallback is called here to update the joypad's set of currently
// pressed keys.
func (j *Joypad) Write(value byte) {
	j.i = 0

	if value&0x1 != 0 {
		j.strobe = true
	} else {
		j.strobe = false
	}

	if j.readKeys != nil {
		j.readKeys()
	}
}
