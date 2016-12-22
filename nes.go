package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/skip2/nes/nes"
)

func main() {
	flag.Parse()

	var args []string = flag.Args()

	if len(args) != 1 {
		fmt.Println("Usage: nes FILENAME.ROM")
		flag.PrintDefaults()
		os.Exit(1)
	}

	var cart *nes.Cartridge

	cart, err := nes.LoadCartridge(args[0])
	if err != nil {
		log.Fatal(err)
	}

	var console *nes.Console = nes.NewConsole(cart)
	var gui *nes.GUI = nes.NewGUI(console)

	err = gui.Run()
	if err != nil {
		log.Fatal(err)
	}
}
