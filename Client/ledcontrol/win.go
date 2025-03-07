package ledcontrol

import (
	"encoding/json"
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

type Config struct {
	LedPin     int `json:"ledPin"`
	LedCount   int `json:"ledCount"`
	Brightness int `json:"brightness"`
}

var dev *ws2811.WS2811
var config Config

// LoadConfig reads config.json and populates the Config struct
func LoadConfig() error {
	file, err := os.Open("config.json")
	if err != nil {
		log.Println("Config file not found, using defaults...")
		config = Config{LedPin: 18, LedCount: 300, Brightness: 50} // Default values
		return nil
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	return nil
}

func InitLEDs() error {
	// Load config first
	if err := LoadConfig(); err != nil {
		return err
	}

	opt := ws2811.DefaultOptions
	opt.Channels[0].GpioPin = config.LedPin
	opt.Channels[0].Brightness = config.Brightness
	opt.Channels[0].LedCount = config.LedCount

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
	go func() {
		if err := InitLEDs(); err != nil {
			log.Fatalf("Error initializing LEDs: %v", err)
		}
		defer CleanupLEDs()

		fmt.Println("Running Celebration LED Animation!")
		celebrateAnimation()
	}()
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
