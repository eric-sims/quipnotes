// app.js
let socket = new WebSocket("ws://localhost:8080/ws?playerID=ericsims");
let selectedWords = [];

socket.onopen = () => {
    console.log("Connected to the WebSocket server");
};

socket.onmessage = (event) => {
    const data = JSON.parse(event.data);

    // If data is the list of tiles, display them
    if (Array.isArray(data)) {
        displayTiles(data);
    }
};

document.getElementById("drawButton").addEventListener("click", () => {
    socket.send(JSON.stringify({ "command": "draw_words", "count": 5 }));
});

document.getElementById("drawOneButton").addEventListener("click", () => {
    socket.send(JSON.stringify({ "command": "draw_words", "count": 1 }));
});

document.getElementById("submitButton").addEventListener("click", () => {
    // Send selected words to the server
    socket.send(JSON.stringify({ "command": "turn_in_ransom_note", "words": selectedWords }));
    selectedWords = [];
    updateSelectedWords();
});

// Display tiles in the container
function displayTiles(tiles) {
    const tileContainer = document.getElementById("tileContainer");
    tileContainer.innerHTML = ""; // Clear previous tiles
    tiles.forEach(tile => {
        const [id, word] = tile.split("|");
        const tileElement = document.createElement("div");
        tileElement.className = "tile";
        tileElement.textContent = word;
        tileElement.dataset.id = id;
        tileElement.addEventListener("click", () => selectTile(id, word));
        tileContainer.appendChild(tileElement);
    });
}

// Add or remove a tile from the selected words list
function selectTile(id, word) {
    const tileString = `${id}|${word}`;
    if (selectedWords.includes(tileString)) {
        selectedWords = selectedWords.filter(item => item !== tileString);
    } else {
        selectedWords.push(tileString);
    }
    updateSelectedWords();
}

// Update the display of selected words
function updateSelectedWords() {
    document.getElementById("selectedWords").textContent = "Selected Words: " +
        selectedWords.map(word => word.split("|")[1]).join(" ");
}

socket.onclose = () => {
    console.log("WebSocket connection closed");
};
