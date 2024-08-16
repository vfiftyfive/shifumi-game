package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
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
		log.Printf("[INFO] New session created: %s", choice.SessionID)
	} else {
		// Check if the game has already ended
		if session.Status == "finished" || session.Status == "Player 1 wins the game!" || session.Status == "Player 2 wins the game!" {
			log.Printf("[ERROR] Attempt to play in a finished game session: %s", choice.SessionID)
			http.Error(w, "Game has already finished", http.StatusConflict)
			return
		}

		if choice.PlayerID == "" {
			// Assign Player 2 ID if Player 1 already exists
			if session.Player1 != nil {
				choice.PlayerID = "2"
			} else {
				choice.PlayerID = "1"
			}
		}

		if (choice.PlayerID == "1" && session.Player1HasPlayed) || (choice.PlayerID == "2" && session.Player2HasPlayed) {
			log.Printf("[ERROR] Player %s has already submitted a choice for this round in session: %s", choice.PlayerID, choice.SessionID)
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

	// Publish the player choice to Kafka
	if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
		log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
		http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
		return
	}

	// Consolidated logic for when both players have played
	if session.Player1HasPlayed && session.Player2HasPlayed {
		determineWinner(session, kafkaBroker) // Determine the winner and update the session once

		session.Round++ // Increment the round after the winner is determined
		session.Player1HasPlayed = false
		session.Player2HasPlayed = false

		// If the game is finished, set the status to prevent further play
		if session.Player1Wins == 3 || session.Player2Wins == 3 {
			session.Status = "finished"
			log.Printf("[INFO] Game finished | SessionID: %s", session.SessionID)
		}
	}

	// Only update the session once after all processing is complete
	updateSession(session, kafkaBroker)

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

func publishPlayerChoice(choice models.PlayerChoice, kafkaBroker string) error {
	writer := kafkago.NewWriter(kafkago.WriterConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    "player-choices",
		Balancer: &kafkago.LeastBytes{},
	})
	defer writer.Close()

	message, err := json.Marshal(choice)
	if err != nil {
		log.Printf(Red+"[ERROR] Failed to marshal player choice: %v"+Reset, err)
		return err
	}

	err = writer.WriteMessages(context.Background(), kafkago.Message{
		Key:   []byte(choice.SessionID),
		Value: message,
	})
	if err != nil {
		log.Printf(Red+"[ERROR] Failed to write player choice to Kafka: %v"+Reset, err)
		return err
	}

	log.Printf(Green+"[INFO] Successfully published player choice | SessionID: %s | PlayerID: %s"+Reset, choice.SessionID, choice.PlayerID)
	return nil
}
