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

var (
	config            Config
	dev               *ws2811.WS2811
	ledMutex          sync.Mutex
	breathingStopChan chan struct{}
	breathingMutex    sync.Mutex
	breathingActive   bool
)

func LoadConfig() error {
	file, err := os.Open("config.json")
	if err != nil {
		log.Println("Config file not found, using defaults...")
		config = Config{LedPin: 18, LedCount: 300, Brightness: 50}
		return nil
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}
	return nil
}

func InitLEDs() error {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if dev != nil {
		return nil // already initialized
	}

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
		return fmt.Errorf("MakeWS2811 failed: %v", err)
	}
	if err := dev.Init(); err != nil {
		return fmt.Errorf("WS2811 Init failed: %v", err)
	}

	log.Printf("InitLEDs: Successfully initialized %d LEDs on GPIO %d", config.LedCount, config.LedPin)
	return nil
}

func CleanupLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	log.Println("Cleaning up.")
	if dev != nil {
		dev.Fini()
		dev = nil
	}
}

func ClearLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if dev == nil {
		log.Println("ClearLEDs: dev is nil")
		return
	}

	leds := dev.Leds(0)
	for i := range leds {
		leds[i] = colorOff
	}
	dev.Render()
}

func RunBreathingEffect() {
	breathingMutex.Lock()
	defer breathingMutex.Unlock()

	if breathingActive {
		log.Println("RunBreathingEffect: already running")
		return
	}

	if dev == nil {
		log.Println("RunBreathingEffect: dev is nil")
		return
	}

	breathingStopChan = make(chan struct{})
	breathingActive = true

	log.Println("RunBreathingEffect: started")
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		var t float64
		for {
			select {
			case <-breathingStopChan:
				log.Println("RunBreathingEffect: stop signal received")
				breathingMutex.Lock()
				breathingActive = false
				breathingMutex.Unlock()
				ClearLEDs()
				log.Println("RunBreathingEffect: exited")
				return
			case <-ticker.C:
				ledMutex.Lock()
				if dev == nil {
					ledMutex.Unlock()
					continue
				}

				leds := dev.Leds(0)
				t += 0.05
				brightness := math.Pow((math.Sin(t)+1.0)/2.0, 2.2)
				color := uint32(0)<<16 | uint32(0)<<8 | uint32(255*brightness)

				for i := 0; i < config.LedCount && i < len(leds); i++ {
					leds[i] = color
				}
				dev.Render()
				ledMutex.Unlock()
			}
		}
	}()
}

func StopBreathingEffect() {
	breathingMutex.Lock()
	defer breathingMutex.Unlock()

	if !breathingActive {
		log.Println("StopBreathingEffect: nothing to stop")
		return
	}

	log.Println("StopBreathingEffect: sending stop")
	close(breathingStopChan)
	breathingStopChan = nil
	breathingActive = false
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

	for _, color := range colors {
		for i := range leds {
			leds[i] = uint32(color)
		}
		dev.Render()
		time.Sleep(1 * time.Second)
	}
	ClearLEDs()
}

func BlinkLEDs() {
	go func() {
		StopBreathingEffect()
		time.Sleep(100 * time.Millisecond) // Give breathing effect time to stop

		if err := InitLEDs(); err != nil {
			log.Printf("BlinkLEDs InitLEDs error: %v", err)
			return
		}

		log.Println("Running Celebration LED Animation!")
		celebrateAnimation()

		RunBreathingEffect()
	}()
}
