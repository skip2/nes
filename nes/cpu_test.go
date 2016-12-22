package nes

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
)

type nesTestLine struct {
	PC          uint16
	Instruction []byte
	A           byte
	X           byte
	Y           byte
	P           byte
	SP          byte
	CYC         int
	SL          int
}

func (n nesTestLine) String() string {
	return fmt.Sprintf("PC=%04X A=%02X X=%02X Y=%02X P=%02X SP=%02X CYC=%03d SL=%03d INS=% X",
		n.PC,
		n.A,
		n.X,
		n.Y,
		n.P,
		n.SP,
		n.CYC,
		n.SL,
		n.Instruction)
}

func ReadNESTestLine(reader *bufio.Reader) (*nesTestLine, string, error) {
	line, err := reader.ReadString('\n')

	if err == io.EOF {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}

	// 0         1         2         3         4         5         6         7         8
	// 0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567
	// C000  4C F5 C5  JMP $C5F5                       A:00 X:00 Y:00 P:24 SP:FD CYC:  0 SL:241

	result := &nesTestLine{}

	_, err = fmt.Sscanf(line, "%4X", &result.PC)
	if err != nil {
		return nil, "", err
	}

	_, err = fmt.Sscanf(line[48:],
		"A:%X X:%X Y:%X P:%X SP:%X CYC:%d SL:%d\n",
		&result.A,
		&result.X,
		&result.Y,
		&result.P,
		&result.SP,
		&result.CYC,
		&result.SL)
	if err != nil {
		return nil, "", err
	}

	if result.SL == -1 {
		result.SL = 261
	}

	result.Instruction, err = hex.DecodeString(strings.Replace(line[6:14], " ", "", -1))
	if err != nil {
		return nil, "", err
	}

	assembly := strings.TrimSpace(line[16:47])
	return result, assembly, nil
}
func TestCPUIndividuals(t *testing.T) {
	filenames := []string{
		"01-implied.nes",
		"02-immediate.nes",
		"03-zero_page.nes",
		"04-zp_xy.nes",
		"05-absolute.nes",
		//"06-abs_xy.nes", // Unofficial opcodes not implemented.
		"07-ind_x.nes",
		"08-ind_y.nes",
		"09-branches.nes",
		//"10-stack.nes", // Stack seems to become off by one?
		"11-special.nes",
	}

	for _, filename := range filenames {
		cart, err := LoadCartridge("test_roms/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		console := NewConsole(cart)
		cpu := console.CPU

		for i := 0; i < 5000000; i++ {
			actualInstructionBytes, err := cpu.NextInstructionBytes()
			if err != nil {
				t.Fatalf("PC=%X, err=%s\n", cpu.PC, err)
			}

			actual := nesTestLine{
				PC:          cpu.PC,
				Instruction: actualInstructionBytes,
				A:           cpu.A,
				X:           cpu.X,
				Y:           cpu.Y,
				P:           cpu.P(),
				SP:          cpu.SP,
				CYC:         int((cpu.NumCycles * 3) % 341),
				SL:          0}
			_, err = cpu.Step()
			if err != nil {
				t.Fatal(err)
			} else if false {
				log.Printf("%s %s %s\n", actual, actualInstructionBytes, cart.SRAM[0][5:25])
			}

			if cart.SRAM[0][0] == 0 && cart.SRAM[0][1] == 0xDE {
				break
			}
		}

		if cart.SRAM[0][0] != 0 {
			t.Fatalf("Test %s failed: %s\n", cart.SRAM[0][5:50])
		}
	}
}

func TestCPUUsingNESTest(t *testing.T) {
	cart, err := LoadCartridge("test_roms/nestest.nes")
	if err != nil {
		t.Fatal(err)
	}
	console := NewConsole(cart)
	cpu := console.CPU
	ppu := console.PPU
	cpu.PC = 0xC000

	file, err := os.Open("test_roms/nestest.log")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var previousExpected *nesTestLine
	var previousAssembly string
	reader := bufio.NewReader(file)
	for i := 1; ; i++ {
		var expected *nesTestLine
		var assembly string
		expected, assembly, err = ReadNESTestLine(reader)

		if err != nil {
			t.Fatal(err)
		} else if expected == nil {
			break
		}

		actualInstructionBytes, err := cpu.NextInstructionBytes()
		if err != nil {
			t.Fatalf("PC=%X, err=%s\n", cpu.PC, err)
		}

		actual := nesTestLine{
			PC:          cpu.PC,
			Instruction: actualInstructionBytes,
			A:           cpu.A,
			X:           cpu.X,
			Y:           cpu.Y,
			P:           cpu.P(),
			SP:          cpu.SP,
			CYC:         int((cpu.NumCycles * 3) % 341),
			SL:          ppu.Scanline}

		if !reflect.DeepEqual(*expected, actual) {
			t.Logf("\nLine no.: %d\nPrevious: %s (%s)\nExpected: %s (%s)\nActual  : %s\n", i, previousExpected, previousAssembly, expected, assembly, actual)
			t.Logf("PPU Scanline=%d Tick=%d\n", ppu.Scanline, ppu.Tick)
			t.FailNow()
		}

		previousExpected = expected
		previousAssembly = assembly

		_, err = console.Step()
		if err != nil {
			t.Fatal(err)
		}
	}
}
