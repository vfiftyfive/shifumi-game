package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"shifumi-game/pkg/kafka"
	"shifumi-game/pkg/models"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Orange = "\033[33m"
)

func ProcessChoices(kafkaBroker string) {
	topic := "player-choices"
	log.Printf(Green+"[INFO] Creating Kafka reader for topic: %s"+Reset, topic)

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    topic,
		GroupID:  "game-logic",
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	defer func() {
		log.Printf(Green+"[INFO] Closing Kafka reader for topic: %s"+Reset, topic)
		reader.Close()
	}()

	for {
		err := kafka.ReadMessages(reader, func(key, value []byte) error {
			log.Printf(Green+"[INFO] Processing message from topic: %s"+Reset, topic)

			// Skip messages where the key or value starts with "test"
			if strings.HasPrefix(string(key), "test") || strings.HasPrefix(string(value), "test") {
				log.Printf(Green+"[INFO] Skipping test message | Key: %s | Value: %s"+Reset, string(key), string(value))
				return nil
			}

			return handlePlayerChoice(key, value, kafkaBroker)
		})
		if err != nil {
			log.Printf(Red+"[ERROR] Error reading messages from topic %s: %v"+Reset, topic, err)
			time.Sleep(2 * time.Second)
		}
	}
}

// handlePlayerChoice function
func handlePlayerChoice(key, value []byte, kafkaBroker string) error {
	var choice models.PlayerChoice
	if err := json.Unmarshal(value, &choice); err != nil {
		log.Printf(Red+"[ERROR] Error unmarshalling player choice | Error: %v"+Reset, err)
		return err
	}
	log.Printf(Green+"[INFO] Successfully unmarshalled player choice | SessionID: %s | PlayerID: %s | Choice: %s"+Reset, choice.SessionID, choice.PlayerID, choice.Choice)

	if !isValidChoice(choice.Choice) {
		log.Printf(Red+"[ERROR] Invalid choice received | Choice: %s | SessionID: %s | PlayerID: %s"+Reset, choice.Choice, choice.SessionID, choice.PlayerID)
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	session, exists := sessions[choice.SessionID]
	if !exists {
		session = &models.GameSession{
			SessionID: choice.SessionID,
			Status:    "in progress",
			Round:     1,
		}
		sessions[choice.SessionID] = session
		log.Printf(Green+"[INFO] New session created | SessionID: %s"+Reset, session.SessionID)
	}

	// Check if the game has already ended
	if session.Status == "Player 1 wins the game!" || session.Status == "Player 2 wins the game!" {
		log.Printf(Red+"[INFO] Game has already ended for session | SessionID: %s"+Reset, session.SessionID)
		session.Player1HasPlayed = false
		session.Player2HasPlayed = false
		return nil
	}

	// Record player choices
	if choice.PlayerID == "1" {
		session.Player1 = &choice
		session.Player1HasPlayed = true
	} else if choice.PlayerID == "2" {
		session.Player2 = &choice
		session.Player2HasPlayed = true
		log.Printf(Green+"[INFO] Both players have played, determining winner | SessionID: %s | Round: %d"+Reset, session.SessionID, session.Round)
		if err := determineWinner(session, kafkaBroker); err != nil {
			log.Printf(Red+"[ERROR] Error determining winner | SessionID: %s | Error: %v"+Reset, session.SessionID, err)
			return err
		}
	}

	// Always update the session state
	if err := updateSession(session, kafkaBroker); err != nil {
		log.Printf(Orange+"[ERROR] Error updating session | SessionID: %s | Error: %v"+Reset, session.SessionID, err)
		return err
	}

	log.Printf(Orange+"[INFO] Session updated successfully | SessionID: %s"+Reset, session.SessionID)
	return nil
}

func updateSession(session *models.GameSession, kafkaBroker string) error {
	topic := "game-results"
	log.Printf(Orange+"[INFO] Updating session | SessionID: %s"+Reset, session.SessionID)

	writer := kafkago.NewWriter(kafkago.WriterConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    topic,
		Balancer: &kafkago.LeastBytes{},
	})
	defer writer.Close()

	sessionBytes, err := json.Marshal(session)
	if err != nil {
		log.Printf(Red+"[ERROR] Failed to marshal session | SessionID: %s | Error: %v"+Reset, session.SessionID, err)
		return err
	}

	if err := writer.WriteMessages(context.Background(), kafkago.Message{
		Key:   []byte(session.SessionID),
		Value: sessionBytes,
	}); err != nil {
		log.Printf(Orange+"[ERROR] Failed to write message to Kafka topic %s | SessionID: %s | Error: %v"+Reset, topic, session.SessionID, err)
		return err
	}
	log.Printf(Orange+"[INFO] Successfully wrote session update to Kafka topic | SessionID: %s"+Reset, session.SessionID)
	return nil
}

func determineWinner(session *models.GameSession, kafkaBroker string) error {
	var resultMessage string

	switch session.Player1Choice {
	case "rock":
		switch session.Player2Choice {
		case "scissors":
			session.Player1Wins++
			resultMessage = "Player 1 wins the round!"
		case "paper":
			session.Player2Wins++
			resultMessage = "Player 2 wins the round!"
		case "rock":
			resultMessage = "It's a draw!"
			session.Draws++
		}
	case "paper":
		switch session.Player2Choice {
		case "rock":
			session.Player1Wins++
			resultMessage = "Player 1 wins the round!"
		case "scissors":
			session.Player2Wins++
			resultMessage = "Player 2 wins the round!"
		case "paper":
			resultMessage = "It's a draw!"
			session.Draws++
		}
	case "scissors":
		switch session.Player2Choice {
		case "paper":
			session.Player1Wins++
			resultMessage = "Player 1 wins the round!"
		case "rock":
			session.Player2Wins++
			resultMessage = "Player 2 wins the round!"
		case "scissors":
			resultMessage = "It's a draw!"
			session.Draws++
		}
	}

	log.Printf(Green+"[INFO] %s | SessionID: %s | Round: %d"+Reset, resultMessage, session.SessionID, session.Round)

	if session.Player1Wins == 3 || session.Player2Wins == 3 {
		session.Status = "finished"
		if session.Player1Wins == 3 {
			session.Status = "Player 1 wins the game!"
		} else {
			session.Status = "Player 2 wins the game!"
		}
		session.Player1HasPlayed = false
		session.Player2HasPlayed = false
		log.Printf(Red+"[INFO] Game over. %s"+Reset, session.Status)
	}

	return updateSession(session, kafkaBroker) // Ensure the final session state is persisted
}

func StatsHandler(w http.ResponseWriter, r *http.Request, kafkaBroker string) {
	log.Println("[INFO] Received request to LiveStatsHandler")

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    "game-results",
		GroupID:  "live-stats-consumer",
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	defer reader.Close()

	w.Header().Set("Content-Type", "application/json")

	encoder := json.NewEncoder(w)
	for {
		msg, err := reader.ReadMessage(r.Context())
		if err != nil {
			log.Printf("[ERROR] Error fetching message from Kafka: %v", err)
			http.Error(w, "Error reading live stats", http.StatusInternalServerError)
			return
		}

		var session models.GameSession
		if err := json.Unmarshal(msg.Value, &session); err != nil {
			log.Printf("[ERROR] Error unmarshalling game session: %v", err)
			continue
		}

		log.Printf("[INFO] Live game session: %v", session)
		encoder.Encode(session)  // Stream each session result as it arrives
		w.(http.Flusher).Flush() // Ensure the data is sent immediately to the client
	}
}
