package client

import (
	"context"
	"encoding/json"
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

// MakechoiceHandler handles player choices and serves the /play api endpoint
func MakeChoiceHandler(w http.ResponseWriter, r *http.Request, kafkaBroker string) {
	log.Println(Green + "[INFO] Received request to MakeChoiceHandler" + Reset)

	// Create a PlayerChoice struct and unmarshall request body into it
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

	// If no session ID is provided, allocate a new session ID if no player ID is provided, else return an error.
	// If a session ID is provided without a player ID this is player 2 joining.
	// If there's already a player 2, session is full.
	// Session ID cannot be provided by the first player joining the game.
	// Player 2 must provide a session ID.

	// Case 1: No session ID is provided
	if choice.SessionID == "" {
		if choice.PlayerID != "" {
			log.Printf("[ERROR] Player ID cannot be provided without a session ID for the first player.")
			http.Error(w, "Player ID cannot be provided without a session ID for the first player.", http.StatusBadRequest)
			return
		}
		// Allocate a new session ID and set Player ID to "1".
		choice.SessionID = generateSessionID()
		choice.PlayerID = "1"
		log.Printf("[INFO] New session created | SessionID: %s", choice.SessionID)
	} else {
		// Case 2: Session ID is set.
		// Retrieve the session ID from the first kafka message in the game-session topic.
		gameSession, err := kafka.ReadGameSession(choice.SessionID, kafkaBroker, kafkago.LastOffset)
		if err != nil {
			log.Printf("[ERROR] Error retrieving game session: %v", err)
			http.Error(w, "Error retrieving game session", http.StatusInternalServerError)
			return
		}
		if gameSession == nil {
			log.Printf("[ERROR] Session ID does not exist or game has not started yet.")
			http.Error(w, "Session ID does not exist or game has not started yet.", http.StatusBadRequest)
			return
		}

		// If PlayerID is not set and session is not full, set Player ID to "2".
		if choice.PlayerID == "" {
			if !gameSession.HasPlayer2Played() {
				choice.PlayerID = "2"
				log.Printf("[INFO] Player 2 joined session | SessionID: %s", choice.SessionID)
			} else {
				log.Printf("[ERROR] Session is full; Player 2 has already joined.")
				http.Error(w, "Session is full; Player 2 has already joined.", http.StatusConflict)
				return
			}
		} else {
			// Case 3: Player ID is provided.
			// Validate the player ID.
			PlayerIDNum, err := strconv.Atoi(choice.PlayerID)
			if err != nil {
				log.Printf("[ERROR] Error converting %s to Integer.", choice.PlayerID)
				http.Error(w, "Error retrieving game session", http.StatusInternalServerError)
				return
			}
			if PlayerIDNum > models.MaxPlayers {
				log.Printf("[ERROR] Invalid PlayerID for session.")
				http.Error(w, "Invalid PlayerID for session.", http.StatusConflict)
				return
			}
		}

		// Check if the game has already finished
		if gameSession.Status == "finished" {
			log.Printf("[ERROR] Attempt to play in a finished game session: %s", choice.SessionID)
			http.Error(w, "Game has already finished", http.StatusConflict)
			return
		}

		// Publish the player choice to Kafka
		if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
			log.Printf("[ERROR] Failed to publish player choice | SessionID: %s | Error: %v", choice.SessionID, err)
			http.Error(w, "Failed to submit choice", http.StatusInternalServerError)
			return
		}

		// Craft the response to the client
		response := map[string]interface{}{
			"session_id": choice.SessionID,
			"player_id":  choice.PlayerID,
			"status":     "Choice submitted successfully",
		}

		// Send the response to the client
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		log.Println("[INFO] Response sent to client")

	}
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
