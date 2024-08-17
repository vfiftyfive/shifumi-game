package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"shifumi-game/pkg/kafka"
	"shifumi-game/pkg/models"
	"strings"
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

// ListenForSessionUpdates listens for session updates on the Kafka topic and updates the local session state
func ListenForSessionUpdates(kafkaBroker string) {
	topic := "game-session"
	log.Printf("[INFO] Creating Kafka consumer for topic: %s", topic)

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    topic,
		GroupID:  "client-session-updates",
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	defer reader.Close()

	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("[ERROR] Error reading message from Kafka: %v", err)
			continue
		}

		var session models.GameSession
		if err := json.Unmarshal(msg.Value, &session); err != nil {
			log.Printf("[ERROR] Error unmarshalling session update: %v", err)
			continue
		}

		mu.Lock()
		sessions[session.SessionID] = &session
		mu.Unlock()

		log.Printf("[INFO] Updated session state | SessionID: %s | Status: %s", session.SessionID, session.Status)
	}
}

// handlePlayerChoice function
func handlePlayerChoice(key, value []byte, kafkaBroker string) error {
	var choice models.PlayerChoice
	if err := json.Unmarshal(value, &choice); err != nil {
		log.Printf(Red+"[ERROR] Error unmarshalling player choice | Error: %v"+Reset, err)
		return err
	}
	log.Printf(Green+"[INFO] Successfully unmarshalled player choice | SessionID: %s | PlayerID: %s | Choice: %s"+Reset, choice.SessionID, choice.PlayerID, formatChoice(choice.Choice))

	// Aggregate choices for this session
	mu.Lock()
	defer mu.Unlock()

	session, exists := sessions[choice.SessionID]
	if !exists {
		session = &models.GameSession{
			SessionID: choice.SessionID,
			Status:    "in progress",
			Round:     1,
			Rounds:    []models.RoundResult{{RoundNumber: 1}}, // Initialize with the first round
		}
		sessions[choice.SessionID] = session
		log.Printf(Green+"[INFO] New session created | SessionID: %s"+Reset, session.SessionID)
	}

	// Ensure the Rounds slice has enough capacity
	if session.Round > len(session.Rounds) {
		// Add a new round if the current round number exceeds the number of rounds in the slice
		session.Rounds = append(session.Rounds, models.RoundResult{RoundNumber: session.Round})
		log.Printf(Green+"[INFO] New round added | RoundNumber: %d | SessionID: %s"+Reset, session.Round, session.SessionID)
	}

	// Assign the choice to the correct player in the current round
	currentRound := &session.Rounds[session.Round-1]
	if choice.PlayerID == "1" {
		currentRound.Player1 = &choice
	} else if choice.PlayerID == "2" {
		currentRound.Player2 = &choice
	}

	// Check if both players have played for the current round
	if currentRound.Player1 != nil && currentRound.Player2 != nil {
		return determineWinner(session, kafkaBroker)
	}

	return nil
}

func updateSession(session *models.GameSession, kafkaBroker string) error {
	topic := "game-session"
	log.Printf(Orange+"[INFO] Updating session | SessionID: %s | Round: %d"+Reset, session.SessionID, session.Round)

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
	currentRound := &session.Rounds[session.Round-1]
	var result string

	if currentRound.Player1 == nil || currentRound.Player2 == nil {
		return fmt.Errorf("incomplete round data: one or both player choices are missing")
	}

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
	session.Round++

	log.Printf(Green+"[INFO] %s | SessionID: %s | Round: %d"+Reset, result, session.SessionID, session.Round-1)

	if session.Player1Wins == 3 || session.Player2Wins == 3 {
		session.Status = "finished"
		log.Printf(Red+"[INFO] Game over. %s"+Reset, session.Status)
	}

	return updateSession(session, kafkaBroker)
}

func formatChoice(choice string) string {
	switch choice {
	case "rock":
		return "🪨"
	case "paper":
		return "📄"
	case "scissors":
		return "X"
	default:
		return choice
	}
}

func StatsHandler(w http.ResponseWriter, r *http.Request, kafkaBroker string) {
	log.Println(Green + "[INFO] Received request to LiveStatsHandler" + Reset)

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    "game-session",
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

		log.Printf(Green+"[INFO] Live game session | SessionID: %s | Round: %d | Status: %s"+Reset, session.SessionID, session.Round-1, session.Status)
		encoder.Encode(session)  // Stream each session result as it arrives
		w.(http.Flusher).Flush() // Ensure the data is sent immediately to the client
	}
}
