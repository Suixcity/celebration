package ledcontrol

import (
	"fmt"
	"log"
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

var dev *ws2811.WS2811

func InitLEDs() error {
	opt := ws2811.DefaultOptions
	opt.Channels[0].GpioPin = ledPin
	opt.Channels[0].Brightness = brightness
	opt.Channels[0].LedCount = ledCount

	var err error
	dev, err = ws2811.MakeWS2811(&opt)
	if err != nil {
		return fmt.Errorf("failed to initialize LEDs: %v", err)
	}
	if err := dev.Init(); err != nil {
		return fmt.Errorf("failed to start LED control: %v", err)
	}
	return nil
}

func CleanupLEDs() {
	fmt.Println("Cleaning up...")
	if dev != nil {
		dev.Fini()
	}
}

func BlinkLEDs() {
	if err := InitLEDs(); err != nil {
		log.Fatal(err)
	}
	defer CleanupLEDs()

	fmt.Println("Running Celebration LED Animation!")
	celebrateAnimation()
}

func celebrateAnimation() {
	colors := []int{colorRed, colorGreen, colorBlue}

	for _, color := range colors {
		for i := 0; i < ledCount; i++ {
			dev.Leds(0)[i] = uint32(color)
		}
		dev.Render()
		time.Sleep(1 * time.Second)
	}

	clearLEDs()
}

func clearLEDs() {
	for i := 0; i < ledCount; i++ {
		dev.Leds(0)[i] = colorOff
	}
	dev.Render()
	time.Sleep(50 * time.Millisecond)
}

