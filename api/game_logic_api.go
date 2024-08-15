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
	Orange = "\033[38;5;214m"
)

func ProcessChoices(kafkaBroker string) {
	topic := "player-choices"
	log.Printf("[INFO] Creating Kafka reader for topic: %s", topic)

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    topic,
		GroupID:  "game-logic",
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	defer func() {
		log.Printf("[INFO] Closing Kafka reader for topic: %s", topic)
		reader.Close()
	}()

	for {
		err := kafka.ReadMessages(reader, func(key, value []byte) error {
			log.Printf("[INFO] Processing message from topic: %s", topic)

			// Skip messages where the key or value starts with "test"
			if strings.HasPrefix(string(key), "test") || strings.HasPrefix(string(value), "test") {
				log.Printf("[INFO] Skipping test message with key: %s, value: %s", string(key), string(value))
				return nil
			}

			return handlePlayerChoice(key, value, kafkaBroker)
		})
		if err != nil {
			log.Printf("[ERROR] Error reading messages from topic %s: %v", topic, err)
			// Sleep briefly to avoid tight looping in case of persistent errors
			time.Sleep(2 * time.Second)
		}
	}
}

// handlePlayerChoice function
func handlePlayerChoice(key, value []byte, kafkaBroker string) error {
	var choice models.PlayerChoice
	if err := json.Unmarshal(value, &choice); err != nil {
		log.Printf("[ERROR] Error unmarshalling player choice: %v", err)
		return err
	}
	log.Printf("[INFO] Successfully unmarshalled player choice: %v", choice)
	log.Printf("[INFO] Processing message with key: %s", string(key))

	if !isValidChoice(choice.Choice) {
		log.Printf("[ERROR] Invalid choice received: %s", choice.Choice)
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
	}

	// Check if the game has already ended
	if session.Status == "Player 1 wins the game!" || session.Status == "Player 2 wins the game!" {
		log.Printf(Red+"[INFO] Game has already ended for session: %s"+Reset, session.SessionID)
		return nil
	}

	// Record player choices
	if choice.PlayerID == "1" {
		session.Player1 = &choice
	} else if choice.PlayerID == "2" {
		session.Player2 = &choice
		determineWinner(session, kafkaBroker) // Only determine winner when both players have made their choices
	}

	// Always update the session
	updateSession(session, kafkaBroker)
	return nil
}

func updateSession(session *models.GameSession, kafkaBroker string) {
	topic := "game-results"

	// Check if the game is finished
	if session.Status == "finished" {
		log.Printf(Green+"[INFO] Session already finished: %s"+Reset, session.SessionID)
	} else {
		log.Printf(Green+"[INFO] Updating session: %s"+Reset, session.SessionID)
	}

	result := models.GameResult{
		SessionID:   session.SessionID,
		Player1Wins: session.Player1Wins,
		Player2Wins: session.Player2Wins,
		Draws:       session.Draws,
		Status:      session.Status, // Retain the current status
		Round:       session.Round,
		Message:     "Waiting for both players",
	}

	writer := kafkago.NewWriter(kafkago.WriterConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    topic,
		Balancer: &kafkago.LeastBytes{},
	})
	defer func() {
		log.Printf(Green+"[INFO] Closing Kafka writer for topic: %s"+Reset, topic)
		writer.Close()
	}()

	resultBytes, _ := json.Marshal(result)
	if err := writer.WriteMessages(context.Background(), kafkago.Message{
		Key:   []byte(session.SessionID),
		Value: resultBytes,
	}); err != nil {
		log.Printf("[ERROR] Failed to write message to Kafka topic %s: %v", topic, err)
		return
	}
	log.Printf(Green+"[INFO] Successfully wrote session update to Kafka topic: %s"+Reset, topic)
}

func determineWinner(session *models.GameSession, kafkaBroker string) {
	var resultMessage string
	var emoji string

	switch session.Player1.Choice {
	case "rock":
		switch session.Player2.Choice {
		case "scissors":
			session.Player1Wins++
			resultMessage = "Player 1 wins the round!"
			emoji = "🪨X → 🥇"
		case "paper":
			session.Player2Wins++
			resultMessage = "Player 2 wins the round!"
			emoji = "🪨📄 → 🥇"
		case "rock":
			session.Draws++
			resultMessage = "It's a draw!"
			emoji = "🪨🪨 → 🤝"
		}
	case "paper":
		switch session.Player2.Choice {
		case "rock":
			session.Player1Wins++
			resultMessage = "Player 1 wins the round!"
			emoji = "📄🪨 → 🥇"
		case "scissors":
			session.Player2Wins++
			resultMessage = "Player 2 wins the round!"
			emoji = "📄X → 🥇"
		case "paper":
			session.Draws++
			resultMessage = "It's a draw!"
			emoji = "📄📄 → 🤝"
		}
	case "scissors":
		switch session.Player2.Choice {
		case "paper":
			session.Player1Wins++
			resultMessage = "Player 1 wins the round!"
			emoji = "X📄 → 🥇"
		case "rock":
			session.Player2Wins++
			resultMessage = "Player 2 wins the round!"
			emoji = "X🪨 → 🥇"
		case "scissors":
			session.Draws++
			resultMessage = "It's a draw!"
			emoji = "XX → 🤝"
		}
	}

	// Check for game end
	if session.Player1Wins == 3 {
		session.Status = "finished"
		resultMessage = "Player 1 wins the game!"
		emoji = "🥇 Player 1 wins the game!"
		log.Printf(Red+"[INFO] End of game for session: %s"+Reset, session.SessionID)
	} else if session.Player2Wins == 3 {
		session.Status = "finished"
		resultMessage = "Player 2 wins the game!"
		emoji = "🥇 Player 2 wins the game!"
		log.Printf(Red+"[INFO] End of game for session: %s"+Reset, session.SessionID)
	} else {
		session.Status = "in progress"
		session.Round++ // Only increment round if the game is still in progress
	}

	// Prepare and log the result
	result := models.GameResult{
		SessionID:   session.SessionID,
		Player1Wins: session.Player1Wins,
		Player2Wins: session.Player2Wins,
		Draws:       session.Draws,
		Status:      session.Status,
		Message:     resultMessage,
		Player1:     session.Player1.Choice,
		Player2:     session.Player2.Choice,
		Round:       session.Round, // This round reflects the current round number
	}

	// Log the content that will be sent to Kafka
	resultBytes, _ := json.Marshal(result)
	log.Printf("[INFO] Sending game result to Kafka topic 'game-results': %s", string(resultBytes))

	// Send the message to Kafka
	writer := kafkago.NewWriter(kafkago.WriterConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    "game-results",
		Balancer: &kafkago.LeastBytes{},
	})
	defer writer.Close()

	kafka.WriteMessages(writer, []byte(session.SessionID), resultBytes)
	log.Printf("[INFO] Game result sent to Kafka: %s", resultBytes)

	// Log the winner or draw
	log.Printf("[INFO] %s", resultMessage)
	log.Printf("[INFO] %s", emoji)

	// Increment the round number for the next round
	if session.Status == "in progress" {
		session.Round++
		log.Printf("[INFO] Ready for the next round in session: %s, Round: %d", session.SessionID, session.Round)
		// Reset players after the round if the game hasn't ended
		session.Player1 = nil
		session.Player2 = nil
	}
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

		var result models.GameResult
		if err := json.Unmarshal(msg.Value, &result); err != nil {
			log.Printf("[ERROR] Error unmarshalling game result: %v", err)
			continue
		}

		log.Printf("[INFO] Live game result: %v", result)
		encoder.Encode(result)   // Stream each result as it arrives
		w.(http.Flusher).Flush() // Ensure the data is sent immediately to the client
	}
}
