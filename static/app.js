// app.js
let socket = new WebSocket("ws://localhost:8080/ws");
let playerName = "";
let selectedWords = [];
let allWords = [];

socket.onopen = () => {
    console.log("Connected to the WebSocket server");
};

socket.onmessage = (event) => {
    const data = JSON.parse(event.data);

    // If data is the list of tiles, display them
    if (Array.isArray(data)) {
        allWords = data;
        displayTiles();
    }
};

document.getElementById("addPlayerButton").addEventListener("click", () => {
    playerName = document.getElementById("playerName").value;
    const gameButtons = document.querySelectorAll(".gameButton")
    gameButtons.forEach((button) => {
        console.log("enabling button", button);
        button.disabled = false;
    })
    const setupButtons = document.querySelectorAll(".setupButton");
    setupButtons.forEach((button) => {
        button.disabled = true;
    })
})

document.getElementById("drawButton").addEventListener("click", () => {
    socket.send(JSON.stringify({ "command": "draw_words", "count": 5, "playerId": playerName }));
});

document.getElementById("drawOneButton").addEventListener("click", () => {
    socket.send(JSON.stringify({ "command": "draw_words", "count": 1, "playerId": playerName }));
});

document.getElementById("resetButton").addEventListener("click", () => {
    selectedWords = [];
    updateSelectedWords();
    displayTiles();
});

document.getElementById("submitButton").addEventListener("click", () => {
    // Send selected words to the server
    socket.send(JSON.stringify({ "command": "turn_in_ransom_note", "words": selectedWords, "playerId": playerName }));
    selectedWords = [];
    updateSelectedWords();
});

// Display tiles in the container
function displayTiles() {
    const tileContainer = document.getElementById("tileContainer");
    tileContainer.innerHTML = ""; // Clear previous tiles
    allWords.forEach(tile => {
        const [id, word] = tile.split("|");
        const tileElement = document.createElement("div");
        tileElement.className = "tile";
        tileElement.textContent = word;
        tileElement.id = id;
        if (selectedWords.includes(tile)) {
            tileElement.style.scale = "0.8"
            tileElement.style.backgroundColor = "#808080"
        }
        tileElement.addEventListener("click", () => selectTile(id, word));
        tileContainer.appendChild(tileElement);
    });
}

// Add or remove a tile from the selected words list
function selectTile(id, word) {
    const tileString = `${id}|${word}`;
    const tileContainer = document.getElementById("tileContainer")
    const tile = document.getElementById(id);

    if (selectedWords.includes(tileString)) {
        // Remove tile from selected words
        selectedWords = selectedWords.filter(item => item !== tileString);

        // Animate back to original size and color
        gsap.to(tile, {
            duration: 0.3,
            scale: 1.0,
            backgroundColor: "#f0f0f0",
            ease: "power2.out"
        });
    } else {
        // Add tile to selected words
        selectedWords.push(tileString);

        // Animate to smaller size and darker color
        gsap.to(tile, {
            duration: 0.3,
            scale: 0.9,
            backgroundColor: "#808080",
            ease: "power2.out"
        });
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
