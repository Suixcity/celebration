package ledcontrol

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sync"
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
var ledMutex sync.Mutex

func LoadConfig() error {
	file, err := os.Open("config.json")
	if err != nil {
		log.Println("Config file not found, using defaults...")
		config = Config{LedPin: 18, LedCount: 300, Brightness: 50}
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
	ledMutex.Lock()
	defer ledMutex.Unlock()
	fmt.Println("Cleaning up...")
	if dev != nil {
		dev.Fini()
	}
}

func BlinkLEDs() {
	go func() {
		StopBreathingEffect()

		if err := InitLEDs(); err != nil {
			log.Fatalf("Error initializing LEDs: %v", err)
		}
		defer CleanupLEDs()

		fmt.Println("Running Celebration LED Animation!")
		celebrateAnimation()

		RunBreathingEffect()
	}()
}

func celebrateAnimation() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if dev == nil {
		log.Println("celebrateAnimation: dev is nil")
		return
	}

	colors := []int{colorRed, colorGreen, colorBlue}
	leds := dev.Leds(0)

	if leds == nil || len(leds) == 0 {
		log.Println("celebrateAnimation: no LEDs found on channel 0")
		return
	}

	for _, color := range colors {
		for i := 0; i < config.LedCount && i < len(leds); i++ {
			leds[i] = uint32(color)
		}
		dev.Render()
		time.Sleep(1 * time.Second)
	}

	ClearLEDs()
}

func ClearLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if dev == nil {
		log.Println("ClearLEDs: dev is nil")
		return
	}

	leds := dev.Leds(0)
	if leds == nil || len(leds) == 0 {
		log.Printf("ClearLEDs: no LEDs found on channel 0 (leds=%v, len=%d)", leds, len(leds))
		return
	}

	for i := 0; i < config.LedCount && i < len(leds); i++ {
		leds[i] = colorOff
	}
	dev.Render()
	time.Sleep(50 * time.Millisecond)
}

var breathingStopChan chan bool

func RunBreathingEffect() {
	if breathingStopChan != nil {
		close(breathingStopChan)
	}
	breathingStopChan = make(chan bool)

	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		var t float64

		for {
			select {
			case <-breathingStopChan:
				ClearLEDs()
				return
			case <-ticker.C:
				if dev == nil {
					continue
				}

				leds := dev.Leds(0)
				if leds == nil || len(leds) == 0 {
					continue
				}

				t += 0.05
				brightness := (math.Sin(t) + 1.0) / 2.0
				brightness = math.Pow(brightness, 2.2)

				r := uint8(0 * brightness)
				g := uint8(0 * brightness)
				b := uint8(255 * brightness)

				color := uint32(r)<<16 | uint32(g)<<8 | uint32(b)

				for i := 0; i < config.LedCount && i < len(leds); i++ {
					leds[i] = color
				}
				dev.Render()
			}
		}
	}()
}

func StopBreathingEffect() {
	if breathingStopChan != nil {
		close(breathingStopChan)
		breathingStopChan = nil
	}
}
