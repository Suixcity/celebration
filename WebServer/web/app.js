const socket = new WebSocket("ws://" + window.location.host + "/ws");

socket.onopen = function() {
    console.log("Connected to WebSocket");
};

socket.onmessage = function(event) {
    console.log("Message from server:", event.data);
};

socket.onerror = function(error) {
    console.log("WebSocket Error:", error);
};

socket.onclose = function() {
    console.log("WebSocket connection closed");
};

function sendCommand(command) {
    if (socket.readyState === WebSocket.OPEN) {
        socket.send(command);
        console.log("Sent command:", command);
    } else {
        alert("WebSocket is not connected!");
    }
}
