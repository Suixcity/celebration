from flask import Flask, request, jsonify
import board
import neopixel
import time

app = Flask(__name__)

# LED Strip Configuration
LED_PIN = board.D18  # GPIO Pin connected to LEDs
NUM_LEDS = 300       # Update this to match your strip length
BRIGHTNESS = 0.5     # 50% brightness
pixels = neopixel.NeoPixel(LED_PIN, NUM_LEDS, brightness=BRIGHTNESS, auto_write=False)

def celebrate():
    """Blink green a few times, then turn off."""
    for _ in range(5):  # Blink 5 times
        pixels.fill((0, 255, 0))  # Green
        pixels.show()
        time.sleep(0.5)  # Stay on for 0.5 sec
        pixels.fill((0, 0, 0))  # Off
        pixels.show()
        time.sleep(0.5)  # Stay off for 0.5 sec

@app.route('/', methods=['POST'])
def webhook():
    data = request.get_json()
    if data and data.get("event") == "closed_won":
        print("🎉 Deal Closed - Blinking LEDs!")
        celebrate()
        return jsonify({"status": "success", "message": "LEDs blinked"}), 200
    return jsonify({"status": "error", "message": "Invalid webhook data"}), 400

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)