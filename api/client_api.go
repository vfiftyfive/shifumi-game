package api

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"shifumi-game/pkg/kafka"
	"shifumi-game/pkg/models"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

var (
	sessions = make(map[string]*models.GameSession)
	mu       sync.Mutex
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func generateSessionID() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func isValidChoice(choice string) bool {
	validChoices := map[string]bool{
		"rock":     true,
		"paper":    true,
		"scissors": true,
	}
	return validChoices[choice]
}

func MakeChoiceHandler(w http.ResponseWriter, r *http.Request, kafkaBroker string) {
	log.Println("[INFO] Received request to MakeChoiceHandler")

	var choice models.PlayerChoice
	err := json.NewDecoder(r.Body).Decode(&choice)
	if err != nil {
		log.Printf("[ERROR] Error decoding player choice: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[INFO] Player choice received: %v", choice)

	// Validate the choice
	if !isValidChoice(choice.Choice) {
		log.Printf("[ERROR] Invalid choice received: %s", choice.Choice)
		http.Error(w, "Invalid choice. Must be rock, paper, or scissors.", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// Check if this is the first player creating a new session
	if choice.SessionID == "" {
		choice.SessionID = generateSessionID()
		choice.PlayerID = "1" // Assign as Player 1
		choice.Round = 1      // Start with round 1 for the first choice
		session := &models.GameSession{
			SessionID: choice.SessionID,
			Status:    "in progress",
			Player1:   &choice,
			Round:     1,
		}
		sessions[choice.SessionID] = session
		log.Printf("[INFO] New session created: %s", choice.SessionID)
	} else {
		// Validate existing session
		session, exists := sessions[choice.SessionID]
		if !exists {
			log.Printf("[ERROR] Invalid session ID: %s", choice.SessionID)
			http.Error(w, "Invalid session ID", http.StatusBadRequest)
			return
		}

		// Check if the game has already finished
		if session.Status == "finished" {
			log.Printf("[ERROR] Attempt to play in a finished game session: %s", choice.SessionID)
			http.Error(w, "Game has already finished", http.StatusConflict)
			return
		}

		// Set the current round for the player's choice
		choice.Round = session.Round

		// Enforce blocking: Prevent the same player from submitting a choice until the other player has played
		if choice.PlayerID == "1" && session.Player1 != nil && session.Round == session.Player1.Round {
			log.Printf("[ERROR] Player 1 has already submitted a choice for this round in session: %s", choice.SessionID)
			http.Error(w, "Player 1 has already submitted a choice", http.StatusConflict)
			return
		} else if choice.PlayerID == "2" && session.Player2 != nil && session.Round == session.Player2.Round {
			log.Printf("[ERROR] Player 2 has already submitted a choice for this round in session: %s", choice.SessionID)
			http.Error(w, "Player 2 has already submitted a choice", http.StatusConflict)
			return
		}

		// Process existing session
		if session.Player1 == nil {
			choice.PlayerID = "1"
			session.Player1 = &choice
			log.Printf("[INFO] Player 1 assigned to session %s", choice.SessionID)
		} else if session.Player2 == nil {
			choice.PlayerID = "2"
			session.Player2 = &choice
			log.Printf("[INFO] Player 2 assigned to session %s", choice.SessionID)
		} else {
			log.Printf("[WARN] Session %s is full", choice.SessionID)
			http.Error(w, "Session is full", http.StatusConflict)
			return
		}
	}

	// Marshal the PlayerChoice struct into JSON
	messageBytes, err := json.Marshal(choice)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal choice: %v", err)
		http.Error(w, "Failed to marshal choice", http.StatusInternalServerError)
		return
	}

	// Write the marshalled JSON to Kafka asynchronously
	writer := kafkago.NewWriter(kafkago.WriterConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    "player-choices",
		Balancer: &kafkago.LeastBytes{},
	})
	defer writer.Close()
	kafka.WriteMessages(writer, []byte(choice.SessionID), messageBytes)
	log.Printf("[INFO] Player choice written to Kafka: %s", messageBytes)

	// Return the session ID and player ID for reference
	response := map[string]interface{}{
		"session_id": choice.SessionID,
		"player_id":  choice.PlayerID,
		"status":     "Choice submitted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
	log.Println("[INFO] Response sent to client")
}
