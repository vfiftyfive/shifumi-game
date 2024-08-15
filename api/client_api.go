package api

import (
	"context"
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

	// If the session ID is empty, create a new session for the first player
	if choice.SessionID == "" {
		choice.SessionID = generateSessionID()
		choice.PlayerID = "1"
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

		// Fetch the latest game result to determine the current round
		currentRound, err := getCurrentRoundFromKafka(session.SessionID, kafkaBroker)
		if err != nil {
			log.Printf("[ERROR] Failed to get current round from Kafka: %v", err)
			http.Error(w, "Failed to determine the current round", http.StatusInternalServerError)
			return
		}

		choice.Round = currentRound

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

// getCurrentRoundFromKafka fetches the latest game-result message from Kafka to determine the current round.
func getCurrentRoundFromKafka(sessionID, kafkaBroker string) (int, error) {
	topic := "game-results"
	partition := 0

	// Create a Kafka reader configured to read from the specific partition and start at the last offset.
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:   []string{kafkaBroker},
		Topic:     topic,
		Partition: partition,
	})
	defer reader.Close()

	// Fetch the last offset
	lastOffset, err := reader.ReadLag(context.Background())
	if err != nil {
		log.Printf(Orange+"[ERROR] Failed to get the last offset: %v"+Reset, err)
		return 0, err
	}

	// Seek to the last offset
	if err := reader.SetOffset(lastOffset - 1); err != nil {
		log.Printf(Orange+"[ERROR] Failed to set offset: %v"+Reset, err)
		return 0, err
	}

	// Read the last message
	msg, err := reader.ReadMessage(context.Background())
	if err != nil {
		log.Printf(Orange+"[ERROR] Failed to read last message from Kafka topic %s: %v"+Reset, topic, err)
		return 0, err
	}

	log.Printf(Orange+"[INFO] Read last message from Kafka topic %s: %s"+Reset, topic, string(msg.Value))

	var result models.GameResult
	if err := json.Unmarshal(msg.Value, &result); err != nil {
		log.Printf(Orange+"[ERROR] Error unmarshalling game result: %v"+Reset, err)
		return 0, err
	}

	if result.SessionID == sessionID {
		log.Printf(Orange+"[INFO] Matching session ID found: %s. Current round: %d"+Reset, result.SessionID, result.Round)
		return result.Round, nil
	}

	log.Printf(Orange+"[INFO] No matching session ID found for %s in the last message", sessionID)
	return 0, nil
}
