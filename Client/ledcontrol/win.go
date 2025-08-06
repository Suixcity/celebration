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
	dev               *ws2811.WS2811
	config            Config
	ledMutex          sync.Mutex
	breathingStopChan chan struct{}
	breathingWg       sync.WaitGroup
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
		return fmt.Errorf("makeWS2811 failed: %v", err)
	}
	if err := dev.Init(); err != nil {
		return fmt.Errorf("init failed: %v", err)
	}

	log.Printf("InitLEDs: %d LEDs on GPIO %d", config.LedCount, config.LedPin)
	return nil
}

func ClearLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()

	if dev == nil {
		log.Println("ClearLEDs: dev is nil")
		return
	}

	leds := dev.Leds(0)
	if len(leds) == 0 {
		log.Println("ClearLEDs: LED array is nil or empty")
		return
	}

	for i := range leds {
		leds[i] = colorOff
	}
	dev.Render()
}

func RunBreathingEffect() {
	StopBreathingEffect()

	breathingStopChan = make(chan struct{})
	log.Println("RunBreathingEffect: starting")

	breathingWg.Add(1)
	go func() {
		defer breathingWg.Done()
		ticker := time.NewTicker(10 * time.Millisecond) // 100 FPS
		defer ticker.Stop()

		var t float64
		baseColor := colorGreen // change to colorBlue etc.

		for {
			select {
			case <-breathingStopChan:
				log.Println("RunBreathingEffect: stopping")
				ClearLEDs()
				return

			case <-ticker.C:
				ledMutex.Lock()
				if dev != nil {
					leds := dev.Leds(0)
					if len(leds) > 0 {
						t += 0.00132 // 30s wave @ 100fps

						// Sine wave [0..1]
						phase := (math.Sin(t) + 1.0) / 2.0

						// Scale to perceptual range [0.2 .. 1.0]
						min := 0.2
						brightness := phase*(1.0-min) + min

						// Base RGB from color constant
						baseR := float64((baseColor >> 16) & 0xFF)
						baseG := float64((baseColor >> 8) & 0xFF)
						baseB := float64(baseColor & 0xFF)

						// Scale + clamp RGB values
						scale := func(v float64) uint32 {
							val := uint32(v * brightness)
							if val < 50 {
								val = 50 // clamp to minimum visible
							}
							return val
						}

						rr := scale(baseR)
						gg := scale(baseG)
						bb := scale(baseB)

						color := (rr << 16) | (gg << 8) | bb

						for i := 0; i < config.LedCount && i < len(leds); i++ {
							leds[i] = color
						}
						dev.Render()
					}
				}
				ledMutex.Unlock()
			}
		}
	}()
}

func StopBreathingEffect() {
	if breathingStopChan != nil {
		log.Println("StopBreathingEffect: signal stop")
		close(breathingStopChan)
		breathingWg.Wait() // block until finished
		breathingStopChan = nil
	}
}

func celebrateAnimation(done chan struct{}) {
	go func() {
		colors := []uint32{colorRed, colorBlue, colorGreen}
		for _, c := range colors {
			ledMutex.Lock()
			if dev == nil {
				ledMutex.Unlock()
				continue
			}
			leds := dev.Leds(0)
			if len(leds) == 0 {
				ledMutex.Unlock()
				continue
			}
			for i := 0; i < config.LedCount && i < len(leds); i++ {
				leds[i] = c
			}
			dev.Render()
			ledMutex.Unlock()
			time.Sleep(time.Second)
		}
		ClearLEDs()
		close(done) // signal animation complete
	}()
}

func BlinkLEDs() {
	log.Println("🎉 Celebration Triggered!")
	StopBreathingEffect()

	done := make(chan struct{})
	celebrateAnimation(done)

	go func() {
		<-done
		RunBreathingEffect()
	}()
}
