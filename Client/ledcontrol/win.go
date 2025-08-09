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

// ShootBounceLEDs: comet bounces 0→end→0 with fading tail, N bounces, then resumes breathing.
func ShootBounceLEDs(headColor uint32, tail int, frameDelay time.Duration, bounces int) {
	log.Println("🏓 Shoot bounce")
	StopBreathingEffect()

	if tail < 1 {
		tail = 1
	}
	if bounces < 1 {
		bounces = 1
	}

	done := make(chan struct{})
	go func() {
		n := config.LedCount
		head := 0
		dir := 1 // +1 forward, -1 backward
		b := 0

		for {
			ledMutex.Lock()
			if dev != nil {
				leds := dev.Leds(0)
				max := min(n, len(leds))
				// clear frame
				for i := 0; i < max; i++ {
					leds[i] = colorOff
				}

				// draw head + tail
				for t := 0; t < tail; t++ {
					pos := head - t*dir // tail follows along direction
					if pos >= 0 && pos < max {
						f := 1.0 - float64(t)/float64(tail)
						leds[pos] = fadeColor(headColor, f)
					}
				}
				dev.Render()
			}
			ledMutex.Unlock()
			time.Sleep(frameDelay)

			// advance
			head += dir
			if head <= 0 {
				head = 0
				dir = +1
				b++
			} else if head >= n-1 {
				head = n - 1
				dir = -1
				b++
			}
			if b >= bounces*2 { // each touch counts
				break
			}
		}

		ClearLEDs()
		close(done)
	}()

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
		8,                   // tail length
		15*time.Millisecond, // speed
		3,                   // blinks at the end
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

// DealWonStackedShootHalfTrigger stacks shots from the END backward.
// A new shot launches whenever the leading shot reaches halfway through
// the *current unfilled region*. Colors alternate from the given palette.
// Ends with color-matched blinking, then resumes breathing.
func DealWonStackedShootHalfTrigger(
	palette []uint32,
	tail int,
	frameDelay time.Duration,
	maxActive int,
	blinkCount int,
	blinkPeriod time.Duration,
) {
	log.Println("🏁 Deal Won → stacked shots (halfway-triggered)")
	StopBreathingEffect()

	if tail < 1 {
		tail = 1
	}
	if maxActive < 1 {
		maxActive = 1
	}
	if len(palette) == 0 {
		palette = []uint32{colorRed, colorBlue, colorGreen}
	}

	type shot struct {
		color uint32
		step  int // head position in "frames" (1 LED per frame)
	}

	done := make(chan struct{})
	go func() {
		n := config.LedCount
		persist := make([]uint32, n) // filled end-region colors
		filledStart := n             // unfilled = [0..filledStart-1]
		colorIdx := 0

		var active []shot
		// seed first shot immediately at the start of the unfilled region
		active = append(active, shot{color: palette[colorIdx%len(palette)], step: 0})
		colorIdx++

		// control flag to avoid multiple spawns before the lead crosses a *new* halfway point
		launchedForThisHalfSegment := false

		for filledStart > 0 || len(active) > 0 {
			// ---- RENDER (nearest-head priority so shots don't visually merge)
			ledMutex.Lock()
			if dev != nil {
				leds := dev.Leds(0)
				max := min(n, len(leds))
				// base = persist (already filled end)
				for i := 0; i < max; i++ {
					leds[i] = persist[i]
				}

				// composite active shots into unfilled region
				const big = 1 << 30
				bestT := make([]int, max)
				for i := 0; i < max; i++ {
					bestT[i] = big
				}
				for si := range active {
					for t := 0; t < tail; t++ {
						pos := active[si].step - t
						if pos < 0 || pos >= filledStart || pos >= max {
							continue
						}
						if t < bestT[pos] {
							bestT[pos] = t
							f := 1.0 - float64(t)/float64(tail)
							leds[pos] = fadeColor(active[si].color, f)
						}
					}
				}
				dev.Render()
			}
			ledMutex.Unlock()
			time.Sleep(frameDelay)

			// ---- ADVANCE heads
			for i := range active {
				active[i].step++
			}

			// ---- EVENT-DRIVEN LAUNCH:
			// If the *leading* head (max step) has reached halfway through the current unfilled region,
			// and capacity allows, launch a new shot at the start (step=0).
			if len(active) > 0 && len(active) < maxActive && filledStart > 0 {
				// leading head = max step among active
				lead := active[0].step
				for i := 1; i < len(active); i++ {
					if active[i].step > lead {
						lead = active[i].step
					}
				}
				half := filledStart / 2
				if lead >= half && !launchedForThisHalfSegment {
					active = append(active, shot{
						color: palette[colorIdx%len(palette)],
						step:  0,
					})
					colorIdx++
					launchedForThisHalfSegment = true
				}
			}

			// ---- COLLECT finished shots and COMMIT chunks to the end
			finishedCount := 0
			for i := range active {
				if active[i].step >= filledStart+tail { // fully passed unfilled region + tail
					finishedCount++
				}
			}
			if finishedCount > 0 {
				next := active[:0]
				// Commit in an alternating palette sequence so adjacent chunks differ
				nextCommitColorIdx := 0
				for _, sh := range active {
					if sh.step >= filledStart+tail {
						chunk := min(tail, filledStart)
						c := palette[(colorIdx+nextCommitColorIdx)%len(palette)]
						// avoid same-as-last color at the boundary
						if filledStart < n && c == persist[filledStart] {
							nextCommitColorIdx++
							c = palette[(colorIdx+nextCommitColorIdx)%len(palette)]
						}
						for j := 0; j < chunk; j++ {
							persist[filledStart-1-j] = c
						}
						filledStart -= chunk
						nextCommitColorIdx++
					} else {
						next = append(next, sh)
					}
				}
				active = next

				// unfilled region changed → reset the halfway launch gate
				launchedForThisHalfSegment = false

				// If region just became empty, break after we show/blink below
				if filledStart <= 0 && len(active) == 0 {
					// fall through to end
				}
			}
		}

		// Show full strip once, then blink using each LED's stacked color (color-matched blink)
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

		for b := 0; b < blinkCount; b++ {
			// OFF
			ledMutex.Lock()
			if dev != nil {
				leds := dev.Leds(0)
				max := min(n, len(leds))
				for i := 0; i < max; i++ {
					leds[i] = colorOff
				}
				dev.Render()
			}
			ledMutex.Unlock()
			time.Sleep(blinkPeriod)

			// ON (persist colors)
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
			time.Sleep(blinkPeriod)
		}

		ClearLEDs()
		close(done)
	}()

	go func() {
		<-done
		RunBreathingEffect()
	}()
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

// RunEffect dispatches simple, composable effects.
// effect: "blink" | "rainbow" | "wipe"
// color:  0xRRGGBB
// cycles: repeats for effects that support it
func RunEffect(effect string, color uint32, cycles int) {
	if err := InitLEDs(); err != nil {
		log.Fatal(err)
	}
	defer ClearLEDs()

	switch effect {
	case "blink":
		if cycles <= 0 {
			cycles = 3
		}
		for c := 0; c < cycles; c++ {
			fill(color)
			dev.Render()
			time.Sleep(500 * time.Millisecond)
			ClearLEDs()
			time.Sleep(250 * time.Millisecond)
		}
	case "wipe":
		if cycles <= 0 {
			cycles = 1
		}
		for c := 0; c < cycles; c++ {
			colorWipe(color, 5*time.Millisecond)
			time.Sleep(200 * time.Millisecond)
			ClearLEDs()
		}
	case "rainbow":
		if cycles <= 0 {
			cycles = 1
		}
		for c := 0; c < cycles; c++ {
			rainbowCycle(2 * time.Millisecond)
		}
	default:
		// fallback to your existing celebrateAnimation
		done := make(chan struct{})
		celebrateAnimation(done)
	}
}

func fill(color uint32) {
	for i := 0; i < config.LedCount; i++ {
		dev.Leds(0)[i] = color
	}
}

func colorWipe(color uint32, delay time.Duration) {
	for i := 0; i < config.LedCount; i++ {
		dev.Leds(0)[i] = color
		dev.Render()
		time.Sleep(delay)
	}
}

func wheel(pos int) uint32 {
	pos = 255 - pos
	switch {
	case pos < 85:
		return uint32((255-pos)<<16 | 0<<8 | pos)
	case pos < 170:
		pos -= 85
		return uint32(0<<16 | pos<<8 | (255 - pos))
	default:
		pos -= 170
		return uint32(pos<<16 | (255-pos)<<8)
	}
}

func rainbowCycle(delay time.Duration) {
	for j := 0; j < 256*3; j++ {
		for i := 0; i < config.LedCount; i++ {
			dev.Leds(0)[i] = wheel((i*256/config.LedCount + j) & 255)
		}
		dev.Render()
		time.Sleep(delay)
	}
}

// =========================
// EFFECT DISPATCHER (append)
// =========================

// RunEffectByName calls your existing effects by name. Add cases as you create more.
func RunEffectByName(effect string, color uint32, cycles int) {
	switch effect {
	// ---- Call your existing named effects (these do their own Init/Cleanup) ----
	case "celebrate_legacy":
		// Uses your existing BlinkLEDs() → celebrateAnimation()
		BlinkLEDs()
		return

	// If you have a custom function already defined, wire it here:
	// case "stacked_shooting":
	// 	StackedShooting()
	// 	return
	// case "knight_rider":
	// 	KnightRider()
	// 	return

	// ---- Generic managed effects (safe wrappers; no conflict with your code) ----
	case "blink":
		runWithDevice(func() {
			if cycles <= 0 {
				cycles = 3
			}
			for c := 0; c < cycles; c++ {
				fill(color)
				dev.Render()
				time.Sleep(500 * time.Millisecond)
				ClearLEDs()
				time.Sleep(250 * time.Millisecond)
			}
		})

	case "wipe":
		runWithDevice(func() {
			if cycles <= 0 {
				cycles = 1
			}
			for c := 0; c < cycles; c++ {
				colorWipe(color, 5*time.Millisecond)
				time.Sleep(200 * time.Millisecond)
				ClearLEDs()
			}
		})

	case "rainbow":
		runWithDevice(func() {
			if cycles <= 0 {
				cycles = 1
			}
			for c := 0; c < cycles; c++ {
				rainbowCycle(2 * time.Millisecond)
			}
		})

	default:
		// Unknown name → fallback to your existing animation
		BlinkLEDs()
	}
}

// runWithDevice wraps short effects that need direct LED access.
func runWithDevice(run func()) {
	if err := InitLEDs(); err != nil {
		log.Fatal(err)
	}
	defer ClearLEDs()
	run()
}
