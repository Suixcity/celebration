package main

import (
	"fmt"
	"log"
	"os"
	"time"

	ws2811 "github.com/rpi-ws281x/rpi-ws281x-go"
)

const (
	ledPin     = 18
	ledCount   = 300
	brightness = 50
	colorRed   = 0xFF0000
	colorGreen = 0x00FF00
	colorBlue  = 0x0000FF
	colorOff   = 0x000000
)

func BlinkLEDs() {
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root. Try: sudo go run led_control.go")
	}

	opt := ws2811.DefaultOptions
	opt.Channels[0].GpioPin = ledPin
	opt.Channels[0].Brightness = brightness
	opt.Channels[0].LedCount = ledCount

	if err := ws2811.Init(opt); err != nil {
		log.Fatalf("Failed to initialize LEDs: %v", err)
	}
	defer func() {
		fmt.Println("Cleaning up...")
		ws2811.Fini()
	}()

	fmt.Println("🎉 Running Celebration LED Animation!")
	celebrateAnimation()
}

func celebrateAnimation() {
	colors := []int{colorRed, colorGreen, colorBlue}

	for _, color := range colors {
		for i := 0; i < ledCount; i++ {
			ws2811.Leds(0)[i] = color
		}
		ws2811.Render()
		time.Sleep(1 * time.Second)
	}

	clearLEDs()
}

func clearLEDs() {
	for i := 0; i < ledCount; i++ {
		ws2811.Leds(0)[i] = colorOff
	}
	ws2811.Render()
	time.Sleep(50 * time.Millisecond)
}
