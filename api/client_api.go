package api

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"shifumi-game/pkg/models"
	"sync"
	"time"
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
		log.Printf("[ERROR] Error decoding player choice | Error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[INFO] Player choice received | SessionID: %s | PlayerID: %s | Choice: %s", choice.SessionID, choice.PlayerID, choice.Choice)

	// Validate the choice
	if !isValidChoice(choice.Choice) {
		log.Printf("[ERROR] Invalid choice received | SessionID: %s | PlayerID: %s | Choice: %s", choice.SessionID, choice.PlayerID, choice.Choice)
		http.Error(w, "Invalid choice. Must be rock, paper, or scissors.", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	session, exists := sessions[choice.SessionID]
	if !exists {
		choice.SessionID = generateSessionID()
		choice.PlayerID = "1"
		session = &models.GameSession{
			SessionID: choice.SessionID,
			Status:    "in progress",
			Round:     1,
		}
		sessions[choice.SessionID] = session
		log.Printf("[INFO] New session created | SessionID: %s", choice.SessionID)
	} else {
		if session.Status == "finished" {
			log.Printf("[ERROR] Attempt to play in a finished game session | SessionID: %s", choice.SessionID)
			http.Error(w, "Game has already finished", http.StatusConflict)
			return
		}

		if (choice.PlayerID == "1" && session.Player1HasPlayed) || (choice.PlayerID == "2" && session.Player2HasPlayed) {
			log.Printf("[WARN] Player %s has already submitted a choice for this round | SessionID: %s", choice.PlayerID, choice.SessionID)
			http.Error(w, fmt.Sprintf("Player %s has already submitted a choice", choice.PlayerID), http.StatusConflict)
			return
		}
	}

	// Record the player's choice
	if choice.PlayerID == "1" {
		session.Player1 = &choice
		session.Player1Choice = choice.Choice
		session.Player1HasPlayed = true
	} else if choice.PlayerID == "2" {
		session.Player2 = &choice
		session.Player2Choice = choice.Choice
		session.Player2HasPlayed = true
	}

	// After both players have played, process the round and prepare for the next one
	if session.Player1HasPlayed && session.Player2HasPlayed {
		log.Printf("[INFO] Both players have played, processing round | SessionID: %s | Round: %d", session.SessionID, session.Round)
		determineWinner(session, kafkaBroker)
		// Reset for the next round
		session.Player1HasPlayed = false
		session.Player2HasPlayed = false
		session.Round++
		log.Printf("[INFO] Preparing for next round | SessionID: %s | Next Round: %d", session.SessionID, session.Round)
	}
	updateSession(session, kafkaBroker)

	response := map[string]interface{}{
		"session_id": choice.SessionID,
		"player_id":  choice.PlayerID,
		"status":     "Choice submitted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
	log.Printf("[INFO] Response sent to client | SessionID: %s | PlayerID: %s", choice.SessionID, choice.PlayerID)
}
