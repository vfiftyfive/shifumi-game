package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"shifumi-game/pkg/kafka"
	"shifumi-game/pkg/models"
	"strconv"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Orange = "\033[33m"
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

// MakeChoiceHandler handles player choices and serves the /play API endpoint
func MakeChoiceHandler(w http.ResponseWriter, r *http.Request, kafkaBroker string) {
	log.Println(Green + "[INFO] Received request to MakeChoiceHandler" + Reset)

	// Create a PlayerChoice struct and unmarshal the request body into it
	var choice models.PlayerChoice
	err := json.NewDecoder(r.Body).Decode(&choice)
	if err != nil {
		log.Printf(Red+"[ERROR] Error decoding player choice: %v"+Reset, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf(Green+"[INFO] Player choice received | PlayerID: %s | SessionID: %s | Choice: %s"+Reset, choice.PlayerID, choice.SessionID, choice.Choice)

	// Validate the choice
	if !isValidChoice(choice.Choice) {
		log.Printf(Red+"[ERROR] Invalid choice received: %s"+Reset, choice.Choice)
		http.Error(w, "Invalid choice. Must be rock, paper, or scissors.", http.StatusBadRequest)
		return
	}

	if choice.SessionID == "" {
		// Case 1: No Session ID provided (First player starts a new session).
		if choice.PlayerID != "" {
			log.Printf("[ERROR] Player ID cannot be provided without a session ID for the first player.")
			http.Error(w, "Player ID cannot be provided without a session ID for the first player.", http.StatusBadRequest)
			return
		}
		// Allocate a new session ID and set Player ID to "1".
		choice.SessionID = generateSessionID()
		choice.PlayerID = "1"
		choice.InitSession = true
		log.Printf("[INFO] New session created | SessionID: %s", choice.SessionID)
	} else {
		// Case 2: Session ID is provided.
		gameSession, err := kafka.ReadGameSession(choice.SessionID, kafkaBroker, "client-reader")
		if err != nil {
			log.Printf("[ERROR] Error retrieving game session: %v", err)
			http.Error(w, "Error retrieving game session", http.StatusInternalServerError)
			return
		}
		if gameSession == nil {
			log.Printf("[ERROR] Session ID does not exist, or the server is busy processing another player's choice.")
			http.Error(w, "Session ID does not exist, or the server is busy processing another player's choice.", http.StatusBadRequest)
			return
		}

		if choice.PlayerID == "" {
			// Player 2 joining.
			if !gameSession.HasPlayer2Played() {
				choice.PlayerID = "2"
				log.Printf("[INFO] Player 2 joined session | SessionID: %s", choice.SessionID)
			} else {
				log.Printf("[ERROR] Session is full; Player 2 has already joined.")
				http.Error(w, "Session is full; Player 2 has already joined.", http.StatusConflict)
				return
			}
		} else {
			// Case 3: Player ID is provided, validate it.
			playerIDNum, err := strconv.Atoi(choice.PlayerID)
			if err != nil {
				log.Printf("[ERROR] Error converting PlayerID %s to integer.", choice.PlayerID)
				http.Error(w, "Invalid PlayerID", http.StatusInternalServerError)
				return
			}
			if playerIDNum > models.MaxPlayers {
				log.Printf("[ERROR] Invalid PlayerID for session.")
				http.Error(w, "Invalid PlayerID for session.", http.StatusConflict)
				return
			}

			// Check if the player has already played in the current round
			if (choice.PlayerID == "1" && gameSession.HasPlayer1Played()) ||
				(choice.PlayerID == "2" && gameSession.HasPlayer2Played()) {
				log.Printf(Red+"[ERROR] Player %s has already played this round | SessionID: %s", choice.PlayerID, choice.SessionID)
				http.Error(w, "Player has already played this round", http.StatusConflict)
				return
			}
		}

		// Check if the game has already finished.
		if gameSession.Status == "finished" {
			log.Printf("[ERROR] Attempt to play in a finished game session: %s", choice.SessionID)
			http.Error(w, fmt.Sprintf("Game has already finished. %s won!", gameSession.Winner), http.StatusConflict)

			return
		}
	}

	// Publish the player choice to Kafka.
	if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
		log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
		http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
		return
	}

	// Respond to the client
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
