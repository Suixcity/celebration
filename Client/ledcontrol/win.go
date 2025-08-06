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
var breathingStopChan chan bool
var breathingRunning bool

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
		log.Printf("InitLEDs: MakeWS2811 failed: %v", err)
		return err
	}
	if err := dev.Init(); err != nil {
		log.Printf("InitLEDs: dev.Init() failed: %v", err)
		return err
	}

	log.Printf("InitLEDs: Successfully initialized %d LEDs on GPIO %d", config.LedCount, config.LedPin)
	return nil
}

func CleanupLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()
	log.Println("Cleaning up...")
	if dev != nil {
		dev.Fini()
		dev = nil
	}
}

func BlinkLEDs() {
	StopBreathingEffect()

	if err := InitLEDs(); err != nil {
		log.Fatalf("BlinkLEDs: Error initializing LEDs: %v", err)
		return
	}

	log.Println("Running Celebration LED Animation!")
	celebrateAnimation()

	if HasValidLEDChannel(0) {
		log.Println("BlinkLEDs: starting RunBreathingEffect")
		RunBreathingEffect()
	} else {
		log.Println("BlinkLEDs: skipping RunBreathingEffect due to invalid LED channel")
	}
}

func celebrateAnimation() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if !HasValidLEDChannel(0) {
		log.Println("celebrateAnimation: invalid LED channel")
		return
	}

	leds := safeLedsSnapshot(0)
	if leds == nil || len(leds) == 0 {
		log.Println("celebrateAnimation: no LEDs found on channel 0")
		return
	}

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

func ClearLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if !HasValidLEDChannel(0) {
		log.Println("ClearLEDs: invalid LED channel")
		return
	}

	leds := safeLedsSnapshot(0)
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

func RunBreathingEffect() {
	if breathingRunning {
		log.Println("RunBreathingEffect: already running, skipping")
		return
	}
	breathingRunning = true
	breathingStopChan = make(chan bool)

	go func() {
		log.Println("RunBreathingEffect: started")
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		var t float64

	loop:
		for {
			select {
			case <-breathingStopChan:
				log.Println("RunBreathingEffect: stop signal received")
				break loop
			case <-ticker.C:
				leds := safeLedsSnapshot(0)
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
		ClearLEDs()
		breathingRunning = false
		log.Println("RunBreathingEffect: exited")
	}()
}

func StopBreathingEffect() {
	if breathingStopChan != nil {
		log.Println("StopBreathingEffect: sending stop")
		close(breathingStopChan)
		breathingStopChan = nil
		time.Sleep(100 * time.Millisecond)
	} else {
		log.Println("StopBreathingEffect: nothing to stop")
	}
	breathingRunning = false
}

func HasValidLEDChannel(channel int) bool {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("HasValidLEDChannel: recovered from panic: %v", r)
		}
	}()
	if dev == nil {
		return false
	}
	leds := dev.Leds(channel)
	return leds != nil && len(leds) > 0
}

func safeLedsSnapshot(channel int) []uint32 {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("safeLedsSnapshot: recovered from panic: %v", r)
		}
	}()
	if dev == nil {
		return nil
	}
	return dev.Leds(channel)
}
