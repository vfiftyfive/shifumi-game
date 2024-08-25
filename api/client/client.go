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

// generateSessionID genereates a new session ID when first player joins
func generateSessionID() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// isValidChoice determines if the player choice is valid
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

	var choice models.PlayerChoice
	err := json.NewDecoder(r.Body).Decode(&choice)
	if err != nil {
		log.Printf(Red+"[ERROR] Error decoding player choice: %v"+Reset, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf(Green+"[INFO] Player choice received | PlayerID: %s | SessionID: %s | Choice: %s"+Reset, choice.PlayerID, choice.SessionID, choice.Choice)

	if !isValidChoice(choice.Choice) {
		log.Printf(Red+"[ERROR] Invalid choice received: %s"+Reset, choice.Choice)
		http.Error(w, "Invalid choice. Must be rock, paper, or scissors.", http.StatusBadRequest)
		return
	}

	if choice.SessionID == "" {
		// Case 1: First player starting a new session
		if choice.PlayerID != "" {
			log.Printf("[ERROR] Player ID cannot be provided without a session ID for the first player.")
			http.Error(w, "Player ID cannot be provided without a session ID for the first player.", http.StatusBadRequest)
			return
		}
		// Initialize new session
		choice.SessionID = generateSessionID()
		choice.PlayerID = "1"
		choice.InitSession = true
		if err := kafka.CreateTopicForSession(kafkaBroker, choice.SessionID, 1, 1); err != nil {
			log.Printf("[ERROR] Error creating topic for session: %v", err)
			http.Error(w, "Error creating Kafka topic", http.StatusInternalServerError)
			return
		}
		log.Printf("[INFO] New session created | SessionID: %s", choice.SessionID)

	} else {
		// Case 2: Existing session, fetch the game session
		var gameSession *models.GameSession
		topicName := "game-results-" + choice.SessionID
		gameSession, err = kafka.ReadGameSession(topicName, choice.SessionID, kafkaBroker, "client-reader")
		if err != nil {
			log.Printf("[ERROR] Error fetching game session: %v", err)
			http.Error(w, "Error retrieving game session", http.StatusInternalServerError)
			return
		}

		if gameSession == nil {
			log.Printf("[INFO] No message found within timeout")
			http.Error(w, "Session ID does not exist, or the server is busy processing another player's choice.", http.StatusBadRequest)
			// Publish the player choice to Kafka
			if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
				log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
				http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
			}
			return
		}

		// Determine if player 2 is joining
		if choice.PlayerID == "" {
			if !gameSession.HasPlayer2Played() && gameSession.HasPlayer1Played() {
				choice.PlayerID = "2"
				log.Printf("[INFO] Player 2 joined session | SessionID: %s", choice.SessionID)
			} else if gameSession.HasPlayer2Played() {
				log.Printf("[ERROR] Session is full; Player 2 has already joined.")
				http.Error(w, "Session is full; Player 2 has already joined.", http.StatusConflict)
				return
			} else {
				log.Printf("[ERROR] Player 2 cannot join yet, waiting for Player 1 to play.")
				http.Error(w, "Player 2 cannot join yet, waiting for Player 1 to play.", http.StatusBadRequest)
				// Publish the player choice to Kafka
				if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
					log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
					http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
				}
				return
			}
		} else {
			// Validate if player ID is within allowed limits
			playerIDNum, err := strconv.Atoi(choice.PlayerID)
			if err != nil || playerIDNum > models.MaxPlayers {
				log.Printf("[ERROR] Invalid PlayerID for session.")
				http.Error(w, "Invalid PlayerID for session.", http.StatusConflict)
				// Publish the player choice to Kafka
				if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
					log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
					http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
				}
				return
			}

			// Check if the player has already played in the current round
			if (choice.PlayerID == "1" && gameSession.HasPlayer1Played()) ||
				(choice.PlayerID == "2" && gameSession.HasPlayer2Played()) {
				log.Printf(Red+"[ERROR] Player %s has already played this round | SessionID: %s", choice.PlayerID, choice.SessionID)
				http.Error(w, "Player has already played this round", http.StatusConflict)
				// Publish the player choice to Kafka
				if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
					log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
					http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
				}
				return
			}
		}

		// Check if the game session has finished
		if gameSession.Status == "finished" {
			log.Printf("[ERROR] Attempt to play in a finished game session: %s", choice.SessionID)
			http.Error(w, fmt.Sprintf("Game has already finished. %s won!", gameSession.Winner), http.StatusConflict)
			return
		}
	}

	// Publish the player choice to Kafka
	if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
		log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
		http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
		return
	}

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

// publishPlayerChoice writes the player choice struct to the player-choices topic
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

	log.Printf(Green+"[INFO] Successfully published player choice | SessionID: %s | PlayerID: %s | Message: %s"+Reset, choice.SessionID, choice.PlayerID, message)
	return nil
}
