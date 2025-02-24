from flask import Flask, request

app = Flask(__name__)

@app.route('/')
def home():
    return "Webhook listener is running!"

@app.route('/webhook', methods=['POST'])
def webhook():
    data = request.json
    print("Received webhook:", data)
    return "Webhook received", 200

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=10000)