package api

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"shifumi-game/pkg/kafka"
	"shifumi-game/pkg/models"
	"time"
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
	var choice models.PlayerChoice
	err := json.NewDecoder(r.Body).Decode(&choice)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !isValidChoice(choice.Choice) {
		http.Error(w, "Invalid choice. Valid choices are: rock, paper, scissors", http.StatusBadRequest)
		return
	}

	if choice.SessionID == "" {
		choice.SessionID = generateSessionID()
	}

	// Marshal the PlayerChoice struct into JSON
	messageBytes, err := json.Marshal(choice)
	if err != nil {
		http.Error(w, "Failed to marshal choice", http.StatusInternalServerError)
		return
	}

	// Create Kafka writer and produce message
	writer := kafka.NewKafkaWriter([]string{kafkaBroker}, "player-choices")
	defer writer.Close()

	err = kafka.WriteMessage(writer, []byte(choice.SessionID), messageBytes)
	if err != nil {
		http.Error(w, "Failed to produce message to Kafka", http.StatusInternalServerError)
		return
	}

	// Return the session ID for reference
	response := map[string]string{
		"session_id": choice.SessionID,
		"status":     "Choice submitted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
