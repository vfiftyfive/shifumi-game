package server

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
	Yellow = "\033[33m"
	Orange = "\033[33m"
)

// ProcessChoices listens to the player-choices topic and processes incoming player choices
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

// handlePlayerChoice processes each player choice, updating the game session and determining the round winner
func handlePlayerChoice(key, value []byte, kafkaBroker string) error {
	var choice models.PlayerChoice
	if err := json.Unmarshal(value, &choice); err != nil {
		log.Printf(Red+"[ERROR] Error unmarshalling player choice | Error: %v"+Reset, err)
		return err
	}
	log.Printf(Green+"[INFO] Successfully unmarshalled player choice | SessionID: %s | PlayerID: %s | Choice: %s"+Reset, choice.SessionID, choice.PlayerID, choice.Choice)

	// Retrieve the game session from Kafka
	gameSession, err := kafka.ReadGameSession(choice.SessionID, kafkaBroker, kafkago.LastOffset)
	if err != nil {
		log.Printf(Red+"[ERROR] Error retrieving game session: %v"+Reset, err)
		return err
	}
	if gameSession == nil {
		log.Printf(Red+"[ERROR] Session ID does not exist: %s"+Reset, choice.SessionID)
		return nil
	}

	// Check if the game has already ended
	if gameSession.Status == "finished" {
		log.Printf(Red+"[ERROR] Game has already ended for session | SessionID: %s"+Reset, gameSession.SessionID)
		return nil
	}

	// Check if the player has already played this round
	if (choice.PlayerID == "1" && gameSession.HasPlayer1Played()) || (choice.PlayerID == "2" && gameSession.HasPlayer2Played()) {
		log.Printf(Yellow+"[INFO] Player %s has already played this round | SessionID: %s"+Reset, choice.PlayerID, choice.SessionID)
		return nil
	}

	// Record the player's choice
	currentRound := &gameSession.Rounds[gameSession.CurrentRound-1]
	if choice.PlayerID == "1" {
		currentRound.Player1 = &choice
		gameSession.SetPlayer1HasPlayed(true)
	} else if choice.PlayerID == "2" {
		currentRound.Player2 = &choice
		gameSession.SetPlayer2HasPlayed(true)
	}

	// If both players have played, determine the winner
	if gameSession.HasPlayer1Played() && gameSession.HasPlayer2Played() {
		determineWinner(gameSession)

		// Prepare for the next round
		gameSession.CurrentRound++
		gameSession.Rounds = append(gameSession.Rounds, models.RoundResult{
			RoundNumber: gameSession.CurrentRound,
		})
		gameSession.SetPlayer1HasPlayed(false)
		gameSession.SetPlayer2HasPlayed(false)
	}

	// Publish the updated game session to Kafka
	if err := updateSession(gameSession, kafkaBroker); err != nil {
		log.Printf(Orange+"[ERROR] Error updating session | SessionID: %s | Error: %v"+Reset, gameSession.SessionID, err)
	}

	log.Printf(Orange+"[INFO] Session updated successfully | SessionID: %s"+Reset, gameSession.SessionID)
	return nil
}

// updateSession publishes the updated game session to the Kafka game-results topic
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

// determineWinner determines the winner of the current round and updates the game session accordingly
func determineWinner(session *models.GameSession) {
	currentRound := &session.Rounds[session.CurrentRound-1]
	var result string

	switch currentRound.Player1.Choice {
	case "rock":
		switch currentRound.Player2.Choice {
		case "scissors":
			session.Player1Wins++
			result = "Player 1 wins 🪨X→ 🥇"
		case "paper":
			session.Player2Wins++
			result = "Player 2 wins 🪨📄 → 🥇"
		case "rock":
			session.Draws++
			result = "Draw 🪨🪨 → 🤝"
		}
	case "paper":
		switch currentRound.Player2.Choice {
		case "rock":
			session.Player1Wins++
			result = "Player 1 wins 📄🪨 → 🥇"
		case "scissors":
			session.Player2Wins++
			result = "Player 2 wins 📄X→ 🥇"
		case "paper":
			session.Draws++
			result = "Draw 📄📄 → 🤝"
		}
	case "scissors":
		switch currentRound.Player2.Choice {
		case "paper":
			session.Player1Wins++
			result = "Player 1 wins X📄 → 🥇"
		case "rock":
			session.Player2Wins++
			result = "Player 2 wins X🪨 → 🥇"
		case "scissors":
			session.Draws++
			result = "Draw XX→ 🤝"
		}
	}

	currentRound.Result = result
	log.Printf(Green+"[INFO] %s | SessionID: %s | Round: %d"+Reset, result, session.SessionID, session.CurrentRound)

	// Check if the game has finished
	if session.Player1Wins == 3 || session.Player2Wins == 3 {
		session.Status = "finished"
		if session.Player1Wins == 3 {
			session.SetWinner("Player 1")
		} else {
			session.SetWinner("Player 2")
		}
		log.Printf(Red+"[INFO] Game over | SessionID: %s | Winner: %s 🥇"+Reset, session.SessionID, session.GetWinner())
	}
}

// StatsHandler handles the /stats API endpoint and streams the game results to the client
func StatsHandler(w http.ResponseWriter, r *http.Request, kafkaBroker string) {
	log.Println(Green + "[INFO] Received request to LiveStatsHandler" + Reset)

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
			log.Printf(Red+"[ERROR] Error fetching message from Kafka: %v"+Reset, err)
			http.Error(w, "Error reading live stats", http.StatusInternalServerError)
			return
		}

		var session models.GameSession
		if err := json.Unmarshal(msg.Value, &session); err != nil {
			log.Printf(Red+"[ERROR] Error unmarshalling game session: %v"+Reset, err)
			continue
		}

		log.Printf(Green+"[INFO] Live game session: %v"+Reset, session)
		encoder.Encode(session)  // Stream each session result as it arrives
		w.(http.Flusher).Flush() // Ensure the data is sent immediately to the client
	}
}
