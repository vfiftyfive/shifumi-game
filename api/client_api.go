package api

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"shifumi-game/pkg/models"
	"sync"
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
	log.Println(Green + "[INFO] Received request to MakeChoiceHandler" + Reset)

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

	// Lock for session management
	mu.Lock()
	defer mu.Unlock()

	// Check if the session already exists
	session, exists := sessions[choice.SessionID]
	if exists {
		// Ensure PlayerID is specified for existing sessions
		if choice.PlayerID == "" {
			log.Printf(Red+"[ERROR] Player ID must be specified for existing session: %s"+Reset, choice.SessionID)
			http.Error(w, "Player ID must be specified for existing sessions.", http.StatusBadRequest)
			return
		}
	} else {
		// If session does not exist, create a new one and assign PlayerID if not provided
		if choice.PlayerID == "" {
			choice.PlayerID = "1" // Default to Player 1 for new sessions
		}
		session = &models.GameSession{
			SessionID: choice.SessionID,
			Status:    "in progress",
			Round:     1,
			Rounds:    []models.RoundResult{{RoundNumber: 1}}, // Initialize with the first round
		}
		sessions[choice.SessionID] = session
		log.Printf(Green+"[INFO] New session created | SessionID: %s | PlayerID: %s"+Reset, session.SessionID, choice.PlayerID)
	}

	// Publish the player choice to Kafka
	if err := publishPlayerChoice(choice, kafkaBroker); err != nil {
		log.Printf(Red+"[ERROR] Failed to publish player choice | SessionID: %s | PlayerID: %s | Error: %v"+Reset, choice.SessionID, choice.PlayerID, err)
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
	log.Printf(Green+"[INFO] Response sent to client | SessionID: %s | PlayerID: %s"+Reset, choice.SessionID, choice.PlayerID)
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
