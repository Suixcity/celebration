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
		baseColor := colorBlue // change to colorBlue etc.

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

						// Scale to perceptual range
						min := 0.2
						brightness := phase*(1.0-min) + min

						// Extract base RGB
						baseR := float64((baseColor >> 16) & 0xFF)
						baseG := float64((baseColor >> 8) & 0xFF)
						baseB := float64(baseColor & 0xFF)

						// Scale + clamp RGB values
						scale := func(v float64) uint32 {
							val := uint32(v * brightness)
							return val
						}

						rr := scale(baseR)
						gg := scale(baseG)
						bb := scale(baseB)

						color := (rr << 16) | (gg << 8) | bb

						/* 🔍 LOG EVERYTHING
						if int(t*1000)%3000 == 0 { // Log roughly every 3 seconds
							log.Printf("t=%.2f phase=%.3f bright=%.3f RGB=(%d,%d,%d) color=0x%06X",
								t, phase, brightness, rr, gg, bb, color)
						}*/

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

// ShootLEDs runs a single "comet" that shoots down the strip with a fading tail,
// then returns to the breathing effect (like BlinkLEDs does).
func ShootLEDs() {
	log.Println("🚀 Shoot effect triggered")
	StopBreathingEffect()

	done := make(chan struct{})
	go shootAnimation(colorBlue, 8, 20*time.Millisecond, done)

	go func() {
		<-done
		RunBreathingEffect()
	}()
}

// shootAnimation animates a bright head with a fading tail from 0..LedCount-1.
func shootAnimation(headColor uint32, tail int, frameDelay time.Duration, done chan struct{}) {
	// safety: tail >= 1
	if tail < 1 {
		tail = 1
	}

	// inclusive travel past the end so the tail fully clears
	totalSteps := config.LedCount + tail

	for step := 0; step < totalSteps; step++ {
		ledMutex.Lock()
		if dev == nil {
			ledMutex.Unlock()
			time.Sleep(frameDelay)
			continue
		}
		leds := dev.Leds(0)
		if len(leds) == 0 {
			ledMutex.Unlock()
			time.Sleep(frameDelay)
			continue
		}

		// clear frame
		max := min(config.LedCount, len(leds))
		for i := 0; i < max; i++ {
			leds[i] = colorOff
		}

		// draw head + tail
		for t := 0; t < tail; t++ {
			pos := step - t
			if pos < 0 || pos >= max {
				continue
			}
			// fade from 1.0 (head) down to ~0
			factor := 1.0 - (float64(t) / float64(tail))
			leds[pos] = fadeColor(headColor, factor)
		}

		dev.Render()
		ledMutex.Unlock()
		time.Sleep(frameDelay)
	}

	ClearLEDs()
	close(done)
}

// fadeColor scales a 0xRRGGBB color by [0..1].
func fadeColor(col uint32, factor float64) uint32 {
	if factor <= 0 {
		return colorOff
	}
	if factor > 1 {
		factor = 1
	}
	r := uint32(float64((col>>16)&0xFF) * factor)
	g := uint32(float64((col>>8)&0xFF) * factor)
	b := uint32(float64(col&0xFF) * factor)
	return (r << 16) | (g << 8) | b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// DealWonStackedShoot stacks comet shots from the END backward until full,
// alternating colors per shot, then blinks 3x and resumes breathing.
func DealWonStackedShoot() {
	log.Println("🏁 Deal Won → Stacked Shoot")
	StopBreathingEffect()

	done := make(chan struct{})
	go shootStackedAnimation(
		[]uint32{colorRed, colorBlue, colorGreen}, // alternating shot colors
		8,                                        // tail length
		15*time.Millisecond,                      // speed
		3,                                        // blinks at the end
		done,
	)

	go func() {
		<-done
		RunBreathingEffect()
	}()
}

func shootStackedAnimation(colors []uint32, tail int, frameDelay time.Duration, blinks int, done chan struct{}) {
	if tail < 1 {
		tail = 1
	}

	n := config.LedCount
	if n <= 0 {
		close(done)
		return
	}

	// persist holds the "filled" region colors that accumulate at the END (highest indexes)
	persist := make([]uint32, n)

	// filledStart is the first index of the filled region [filledStart..n-1]
	filledStart := n
	colorIdx := 0

	for filledStart > 0 {
		shotColor := colors[colorIdx%len(colors)]
		colorIdx++

		// Animate a single comet through the unfilled region (0..filledStart-1)
		for step := 0; step < filledStart+tail; step++ {
			ledMutex.Lock()
			if dev == nil {
				ledMutex.Unlock()
				time.Sleep(frameDelay)
				continue
			}
			leds := dev.Leds(0)
			max := min(n, len(leds))

			// base frame = persistent filled region (at the end)
			for i := 0; i < max; i++ {
				leds[i] = persist[i]
			}

			// draw moving comet only in the UNFILLED region
			for t := 0; t < tail; t++ {
				pos := step - t
				if pos < 0 || pos >= filledStart || pos >= max {
					continue
				}
				factor := 1.0 - (float64(t) / float64(tail)) // head=1 → tail→0
				leds[pos] = fadeColor(shotColor, factor)
			}

			dev.Render()
			ledMutex.Unlock()
			time.Sleep(frameDelay)
		}

		// Commit this shot as a new chunk at the END, growing backward
		chunk := min(tail, filledStart)
		for i := 0; i < chunk; i++ {
			persist[filledStart-1-i] = shotColor
		}
		filledStart -= chunk
	}

	// Show the fully filled strip once
	ledMutex.Lock()
	if dev != nil {
		leds := dev.Leds(0)
		max := min(n, len(leds))
		for i := 0; i < max; i++ {
			leds[i] = persist[i]
		}
		dev.Render()
	}
	ledMutex.Unlock()

	// Blink 3x (white) before returning to normal state
	blinkStrip(blinks, 0xFFFFFF, 220*time.Millisecond)

	ClearLEDs()
	close(done)
}

func blinkStrip(times int, onColor uint32, period time.Duration) {
	for i := 0; i < times; i++ {
		ledMutex.Lock()
		if dev != nil {
			leds := dev.Leds(0)
			max := min(config.LedCount, len(leds))
			for i := 0; i < max; i++ {
				leds[i] = onColor
			}
			dev.Render()
		}
		ledMutex.Unlock()
		time.Sleep(period)

		ledMutex.Lock()
		if dev != nil {
			leds := dev.Leds(0)
			max := min(config.LedCount, len(leds))
			for i := 0; i < max; i++ {
				leds[i] = colorOff
			}
			dev.Render()
		}
		ledMutex.Unlock()
		time.Sleep(period)
	}
}