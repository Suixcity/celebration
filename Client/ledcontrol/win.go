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
	dev             *ws2811.WS2811
	config          Config
	ledMutex        sync.Mutex
	breathingStop   chan struct{}
	breathingActive bool
	breathingLock   sync.Mutex
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
		return fmt.Errorf("failed to parse config: %v", err)
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

	log.Printf("InitLEDs: Successfully initialized %d LEDs on GPIO %d", config.LedCount, config.LedPin)
	return nil
}

func CleanupLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if dev != nil {
		dev.Fini()
	}
}

func ClearLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	leds := dev.Leds(0)
	for i := 0; i < config.LedCount && i < len(leds); i++ {
		leds[i] = colorOff
	}
	dev.Render()
}

func celebrateAnimation() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	leds := dev.Leds(0)
	colors := []int{colorRed, colorGreen, colorBlue}

	for _, color := range colors {
		for i := 0; i < config.LedCount && i < len(leds); i++ {
			leds[i] = uint32(color)
		}
		dev.Render()
		time.Sleep(1 * time.Second)
	}

	ClearLEDs()
}

func StopBreathingEffect() {
	breathingLock.Lock()
	defer breathingLock.Unlock()

	if breathingActive && breathingStop != nil {
		log.Println("StopBreathingEffect: sending stop")
		close(breathingStop)
		breathingStop = nil
		breathingActive = false
	} else {
		log.Println("StopBreathingEffect: nothing to stop")
	}
}

func RunBreathingEffect() {
	breathingLock.Lock()
	defer breathingLock.Unlock()

	if breathingActive {
		log.Println("RunBreathingEffect: already running, skipping")
		return
	}

	breathingActive = true
	breathingStop = make(chan struct{})
	log.Println("RunBreathingEffect: started")

	go func() {
		defer func() {
			log.Println("RunBreathingEffect: exited")
			breathingLock.Lock()
			breathingActive = false
			breathingLock.Unlock()
		}()

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		var t float64
		for {
			select {
			case <-breathingStop:
				log.Println("RunBreathingEffect: stop signal received")
				ClearLEDs()
				return
			case <-ticker.C:
				ledMutex.Lock()
				leds := dev.Leds(0)
				brightness := math.Pow((math.Sin(t)+1.0)/2.0, 2.2)
				color := uint32(0)<<16 | uint32(0)<<8 | uint32(255*brightness)
				for i := 0; i < config.LedCount && i < len(leds); i++ {
					leds[i] = color
				}
				dev.Render()
				ledMutex.Unlock()
				t += 0.05
			}
		}
	}()
}

func BlinkLEDs() {
	log.Println("🎉 Celebration Triggered!")
	StopBreathingEffect()

	go func() {
		celebrateAnimation()
		log.Println("BlinkLEDs: restarting breathing effect after animation")
		RunBreathingEffect()
	}()
}
