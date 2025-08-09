package ledcontrol

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	ws2811 "github.com/rpi-ws281x/rpi-ws281x-go"
)

//
// =======================
//  Hardware & Base Config
// =======================
//

const (
	colorRed   uint32 = 0xFF0000
	colorGreen uint32 = 0x00FF00
	colorBlue  uint32 = 0x0000FF
	colorOff   uint32 = 0x000000
)

type idleCfg struct {
	Color string `json:"color"` // "#RRGGBB" breathing color
}

type Config struct {
	LedPin     int     `json:"ledPin"`
	LedCount   int     `json:"ledCount"`
	Brightness int     `json:"brightness"` // 0..255 (driver scales)
	Idle       idleCfg `json:"idle"`
}

var (
	dev      *ws2811.WS2811
	config   = Config{LedPin: 18, LedCount: 300, Brightness: 255}
	ledMutex sync.Mutex
)

func LoadConfig() error {
	f, err := os.Open("config.json")
	if err != nil {
		log.Println("config.json not found; using hardware defaults.")
		return nil
	}
	defer f.Close()

	var tmp Config
	if err := json.NewDecoder(f).Decode(&tmp); err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}
	if tmp.LedPin != 0 {
		config.LedPin = tmp.LedPin
	}
	if tmp.LedCount != 0 {
		config.LedCount = tmp.LedCount
	}
	if tmp.Brightness != 0 {
		config.Brightness = tmp.Brightness
	}
	config.Idle.Color = strings.TrimSpace(tmp.Idle.Color)
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
		return fmt.Errorf("ws2811 init failed: %v", err)
	}
	log.Printf("LEDs init: %d LEDs on GPIO %d (brightness %d)", config.LedCount, config.LedPin, config.Brightness)
	return nil
}

// EnsureInit initializes the device if needed.
func EnsureInit() error {
	ledMutex.Lock()
	defer ledMutex.Unlock()
	if dev != nil {
		return nil
	}
	return InitLEDs()
}

func CleanupLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()
	if dev != nil {
		// optional: clear before shutdown
		leds := dev.Leds(0)
		for i := range leds {
			leds[i] = colorOff
		}
		dev.Render()
		dev.Fini()
		dev = nil
	}
}

func ClearLEDs() {
	ledMutex.Lock()
	defer ledMutex.Unlock()
	if dev == nil {
		return
	}
	leds := dev.Leds(0)
	for i := range leds {
		leds[i] = colorOff
	}
	dev.Render()
}

// parseHexColor parses "#RRGGBB" or "RRGGBB" into 0xRRGGBB as uint32.
func parseHexColor(s string) uint32 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return 0
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}

//
// ==================
//  Idle: Breathing
// ==================
//

var (
	breathingStopChan chan struct{}
	breathingWg       sync.WaitGroup
)

// scaleColorWithFloor scales 0xRRGGBB by gain [0..1], ensuring each nonzero
// channel is at least floorLSB when gain > 0 (pre‑compensated for global brightness).
func scaleColorWithFloor(color uint32, gain float64, floorLSB uint32) uint32 {
	if gain <= 0 {
		return colorOff
	}
	if gain > 1 {
		gain = 1
	}
	baseR := uint32((color >> 16) & 0xFF)
	baseG := uint32((color >> 8) & 0xFF)
	baseB := uint32(color & 0xFF)

	scale := func(v uint32) uint32 {
		if v == 0 {
			return 0
		}
		s := uint32(float64(v) * gain)
		if s == 0 {
			s = floorLSB // keep at least N LSB pre‑global‑brightness
		}
		if s > 255 {
			s = 255
		}
		return s
	}

	r := scale(baseR)
	g := scale(baseG)
	b := scale(baseB)
	return (r << 16) | (g << 8) | b
}

// minLSBFromGlobal returns the minimum pre‑brightness LSB that will survive
// the ws281x driver’s global brightness scaling (uses >>8, i.e. /256).
func minLSBFromGlobal() uint32 {
	b := config.Brightness
	if b <= 0 || b >= 255 {
		return 1
	}
	// ceil(256 / b)
	return uint32((256 + b - 1) / b)
}

func setAllLEDs(col uint32) {
	ledMutex.Lock()
	defer ledMutex.Unlock()
	if dev == nil {
		return
	}
	leds := dev.Leds(0)
	max := min(config.LedCount, len(leds))
	for i := 0; i < max; i++ {
		leds[i] = col
	}
	dev.Render()
}

func RunBreathingEffect() {
	StopBreathingEffect()
	if err := EnsureInit(); err != nil {
		log.Printf("RunBreathingEffect: init failed: %v", err)
		return
	}

	// Pick idle color from config, fallback to blue
	baseColor := parseHexColor(config.Idle.Color)
	if baseColor == 0 {
		baseColor = colorBlue
	}

	// Pre‑compensated floor so global brightness won’t zero it out
	floor := minLSBFromGlobal()

	breathingStopChan = make(chan struct{})
	log.Println("RunBreathingEffect: starting")

	breathingWg.Add(1)
	go func() {
		defer breathingWg.Done()

		const frame = 10 * time.Millisecond // ~100 fps
		ticker := time.NewTicker(frame)
		defer ticker.Stop()

		const secondsPerCycle = 12.0
		const minDuty = 0.10 // 10% base so it never “looks off”
		omega := 2 * math.Pi / secondsPerCycle
		start := time.Now()

		for {
			select {
			case <-breathingStopChan:
				log.Println("RunBreathingEffect: stopping")
				ClearLEDs()
				return

			case now := <-ticker.C:
				elapsed := now.Sub(start).Seconds()
				// 0..1 sine wave, eased near the bottom for nicer linger
				phase := (math.Sin(omega*elapsed) + 1.0) / 2.0
				phase = phase * phase
				brightness := minDuty + (1.0-minDuty)*phase

				col := scaleColorWithFloor(baseColor, brightness, floor)
				setAllLEDs(col)
			}
		}
	}()
}

func StopBreathingEffect() {
	if breathingStopChan != nil {
		log.Println("StopBreathingEffect: signal stop")
		close(breathingStopChan)
		breathingWg.Wait()
		breathingStopChan = nil
	}
}

//
// =======================
//  Core “Celebrate” Demo
// =======================
//

func celebrateAnimation(done chan struct{}) {
	go func() {
		colors := []uint32{colorRed, colorBlue, colorGreen}
		for _, c := range colors {
			ledMutex.Lock()
			if dev != nil {
				leds := dev.Leds(0)
				max := min(config.LedCount, len(leds))
				for i := 0; i < max; i++ {
					leds[i] = c
				}
				dev.Render()
			}
			ledMutex.Unlock()
			time.Sleep(time.Second)
		}
		ClearLEDs()
		close(done)
	}()
}

func BlinkLEDs() {
	log.Println("🎉 Celebration Triggered!")

	if err := EnsureInit(); err != nil {
		log.Printf("BlinkLEDs: init failed: %v", err)
		return
	}

	done := make(chan struct{})
	celebrateAnimation(done)
	<-done
}

//
// =======================
//  Shoot / Comet Effects
// =======================
//

func ShootLEDs() {
	log.Println("🚀 Shoot effect triggered")

	if err := EnsureInit(); err != nil {
		log.Printf("ShootLEDs: init failed: %v", err)
		return
	}

	done := make(chan struct{})
	go shootAnimation(colorBlue, 8, 20*time.Millisecond, done)

	<-done
}

func ShootBounceLEDs(headColor uint32, tail int, frameDelay time.Duration, bounces int) {
	log.Println("🏓 Shoot bounce")

	if err := EnsureInit(); err != nil {
		log.Printf("ShootBounceLEDs: init failed: %v", err)
		return
	}

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
					pos := head - t*dir
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
			if b >= bounces*2 {
				break
			}
		}

		ClearLEDs()
		close(done)
	}()

	<-done
}

func shootAnimation(headColor uint32, tail int, frameDelay time.Duration, done chan struct{}) {
	if tail < 1 {
		tail = 1
	}
	totalSteps := config.LedCount + tail

	for step := 0; step < totalSteps; step++ {
		ledMutex.Lock()
		if dev != nil {
			leds := dev.Leds(0)
			max := min(config.LedCount, len(leds))

			// clear
			for i := 0; i < max; i++ {
				leds[i] = colorOff
			}
			// head + tail
			for t := 0; t < tail; t++ {
				pos := step - t
				if pos < 0 || pos >= max {
					continue
				}
				f := 1.0 - float64(t)/float64(tail)
				leds[pos] = fadeColor(headColor, f)
			}
			dev.Render()
		}
		ledMutex.Unlock()
		time.Sleep(frameDelay)
	}

	ClearLEDs()
	close(done)
}

//
// ======================
//  Stacked Shoot Effects
// ======================
//

func DealWonStackedShoot() {
	log.Println("🏁 Deal Won → Stacked Shoot")

	if err := EnsureInit(); err != nil {
		log.Printf("DealWonStackedShoot: init failed: %v", err)
		return
	}

	done := make(chan struct{})
	go shootStackedAnimation(
		[]uint32{colorRed, colorBlue, colorGreen},
		8,
		15*time.Millisecond,
		3,
		done,
	)

	<-done
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

	// persistent fill region at END
	persist := make([]uint32, n)
	filledStart := n // unfilled = [0..filledStart-1]
	colorIdx := 0

	for filledStart > 0 {
		shotColor := colors[colorIdx%len(colors)]
		colorIdx++

		// animate comet through unfilled region
		for step := 0; step < filledStart+tail; step++ {
			ledMutex.Lock()
			if dev != nil {
				leds := dev.Leds(0)
				max := min(n, len(leds))
				for i := 0; i < max; i++ {
					leds[i] = persist[i]
				}
				for t := 0; t < tail; t++ {
					pos := step - t
					if pos < 0 || pos >= filledStart || pos >= max {
						continue
					}
					f := 1.0 - float64(t)/float64(tail)
					leds[pos] = fadeColor(shotColor, f)
				}
				dev.Render()
			}
			ledMutex.Unlock()
			time.Sleep(frameDelay)
		}

		// commit chunk to end
		chunk := min(tail, filledStart)
		for i := 0; i < chunk; i++ {
			persist[filledStart-1-i] = shotColor
		}
		filledStart -= chunk
	}

	// show full
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

	// blink
	blinkStrip(blinks, 0xFFFFFF, 220*time.Millisecond)

	ClearLEDs()
	close(done)
}

//
// =======================
//  Generic Effect Helpers
// =======================
//

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

func blinkStrip(times int, onColor uint32, period time.Duration) {
	for i := 0; i < times; i++ {
		ledMutex.Lock()
		if dev != nil {
			leds := dev.Leds(0)
			max := min(config.LedCount, len(leds))
			for j := 0; j < max; j++ {
				leds[j] = onColor
			}
			dev.Render()
		}
		ledMutex.Unlock()
		time.Sleep(period)

		ledMutex.Lock()
		if dev != nil {
			leds := dev.Leds(0)
			max := min(config.LedCount, len(leds))
			for j := 0; j < max; j++ {
				leds[j] = colorOff
			}
			dev.Render()
		}
		ledMutex.Unlock()
		time.Sleep(period)
	}
}

func fill(color uint32) {
	ledMutex.Lock()
	defer ledMutex.Unlock()
	if dev == nil {
		return
	}
	leds := dev.Leds(0)
	max := min(config.LedCount, len(leds))
	for i := 0; i < max; i++ {
		leds[i] = color
	}
}

func colorWipe(color uint32, delay time.Duration) {
	for i := 0; i < config.LedCount; i++ {
		ledMutex.Lock()
		if dev != nil {
			dev.Leds(0)[i] = color
			dev.Render()
		}
		ledMutex.Unlock()
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
		ledMutex.Lock()
		if dev != nil {
			leds := dev.Leds(0)
			max := min(config.LedCount, len(leds))
			for i := 0; i < max; i++ {
				leds[i] = wheel((i*256/config.LedCount + j) & 255)
			}
			dev.Render()
		}
		ledMutex.Unlock()
		time.Sleep(delay)
	}
}

//
// =============================
//  Public Effect Dispatchers
// =============================
//

func RunEffect(effect string, color uint32, cycles int) {
	StopBreathingEffect()
	if err := EnsureInit(); err != nil {
		log.Printf("RunEffect(%s): init failed: %v", effect, err)
		return
	}
	defer func() {
		ClearLEDs()
		CleanupLEDs()
	}()

	switch effect {
	case "blink":
		if cycles <= 0 {
			cycles = 3
		}
		for c := 0; c < cycles; c++ {
			fill(color)
			ledMutex.Lock()
			if dev != nil {
				dev.Render()
			}
			ledMutex.Unlock()
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
		// fallback to your existing celebrate
		done := make(chan struct{})
		celebrateAnimation(done)
		<-done
	}
}

func RunEffectByName(effect string, color uint32, cycles int) {
	switch effect {
	case "celebrate_legacy":
		BlinkLEDs()
		return
	case "shoot":
		ShootLEDs()
		return
	case "shoot_bounce":
		ShootBounceLEDs(colorBlue, 8, 15*time.Millisecond, 4)
		return
	case "stacked_shooting", "deal_won_stacked":
		DealWonStackedShoot()
		return

	case "blink", "wipe", "rainbow":
		RunEffect(effect, color, cycles)
		return

	default:
		BlinkLEDs()
	}
}
