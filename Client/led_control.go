package main

import (
	"fmt"
	"log"
	"time"

	ws2811 "github.com/rpi-ws281x/rpi-ws281x-go"
)

const (
	ledPin     = 18  // GPIO Pin (matches your Python setup)
	ledCount   = 300 // Number of LEDs in your strip
	brightness = 50  // Adjust brightness (0-255)
	colorRed   = 0xFF0000
	colorGreen = 0x00FF00
	colorBlue  = 0x0000FF
	colorOff   = 0x000000
)

func main() {
	opt := ws2811.DefaultOptions
	opt.Channels[0].GpioPin = ledPin
	opt.Channels[0].Brightness = brightness
	opt.Channels[0].LedCount = ledCount

	if err := ws2811.Init(opt); err != nil {
		log.Fatalf("Failed to initialize LEDs: %v", err)
	}
	defer ws2811.Fini()

	fmt.Println("🎉 Running Celebration LED Animation!")
	celebrateAnimation()
}

func celebrateAnimation() {
	for i := 0; i < ledCount; i++ {
		ws2811.SetLed(i, colorRed)
		ws2811.Render()
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(1 * time.Second)

	for i := 0; i < ledCount; i++ {
		ws2811.SetLed(i, colorGreen)
		ws2811.Render()
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(1 * time.Second)

	for i := 0; i < ledCount; i++ {
		ws2811.SetLed(i, colorBlue)
		ws2811.Render()
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(1 * time.Second)
	clearLEDs()
}

func clearLEDs() {
	for i := 0; i < ledCount; i++ {
		ws2811.SetLed(i, colorOff)
	}
	ws2811.Render()
}
