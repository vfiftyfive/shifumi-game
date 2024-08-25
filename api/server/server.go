package server

import (
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

var mu sync.Mutex

// ProcessChoices listens to the player-choices topic and processes incoming player choices
func ProcessChoices(kafkaBroker string) {
	topic := "player-choices"
	backoff := 2 * time.Second // Initial backoff duration

	for {
		log.Printf(Green+"[INFO] Creating Kafka reader for topic: %s"+Reset, topic)

		reader := kafkago.NewReader(kafkago.ReaderConfig{
			Brokers:  []string{kafkaBroker},
			Topic:    topic,
			GroupID:  "game-logic",
			MinBytes: 10e3, // 10KB
			MaxBytes: 10e6, // 10MB
		})

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
				log.Printf(Red+"[ERROR] Error reading messages from topic %s: %v. Retrying in %s"+Reset, topic, err, backoff)
				time.Sleep(backoff)
				if backoff < 1*time.Minute {
					backoff *= 2 // Exponential backoff, with a cap at 1 minute
				}
				break // Exit the inner loop to recreate the reader and reconnect
			}
			// Reset backoff after a successful read
			backoff = 2 * time.Second
		}

		log.Printf(Red+"[ERROR] Reconnecting Kafka reader for topic %s due to persistent errors"+Reset, topic)
		reader.Close()
		time.Sleep(backoff)
	}
}

// handlePlayerChoice processes each player choice, updating the game session and determining the round winner
func handlePlayerChoice(key, value []byte, kafkaBroker string) error {
	mu.Lock()         // Lock the mutex
	defer mu.Unlock() // Ensure the mutex is unlocked when function exits
	var choice models.PlayerChoice
	if err := json.Unmarshal(value, &choice); err != nil {
		log.Printf(Red+"[ERROR] Error unmarshalling player choice | Error: %v"+Reset, err)
		return err
	}
	log.Printf(Green+"[INFO] Successfully unmarshalled player choice | SessionID: %s | PlayerID: %s | Choice: %s"+Reset, choice.SessionID, choice.PlayerID, choice.Choice)

	var gameSession *models.GameSession
	var err error
	topicName := "game-results-" + choice.SessionID

	if choice.InitSession {
		gameSession = models.NewGameSession(choice.SessionID)
		log.Printf(Green+"[INFO] New game session created | SessionID: %s"+Reset, choice.SessionID)
	} else {
		gameSession, err = kafka.ReadGameSession(topicName, choice.SessionID, kafkaBroker, "server")
		if err != nil {
			log.Printf(Red+"[ERROR] Error retrieving game session: %v"+Reset, err)
			return err
		}
	}

	// Record the player's choice

	if gameSession == nil {
		log.Printf(Red+"[ERROR] Invalid game session state | SessionID: %s"+Reset, choice.SessionID)
		return fmt.Errorf("invalid game session state for sessionID: %s", choice.SessionID)
	}

	currentRound := &gameSession.Results[gameSession.CurrentRound-1]
	if choice.PlayerID == "1" {
		currentRound.Player1 = &choice
		gameSession.SetPlayer1HasPlayed(true)
		log.Printf(Yellow+"[INFO] Player 1 has played | SessionID: %s"+Reset, choice.SessionID)

	} else if choice.PlayerID == "2" {
		currentRound.Player2 = &choice
		gameSession.SetPlayer2HasPlayed(true)
		log.Printf(Yellow+"[INFO] Player 2 has played | SessionID: %s"+Reset, choice.SessionID)
	}

	// Before determining the winner, log the current state
	log.Printf(Green+"[INFO] Before determining winner | Round: %d | Player 1 Played: %t | Player 2 Played: %t"+Reset,
		gameSession.CurrentRound, gameSession.HasPlayer1Played(), gameSession.HasPlayer2Played())

	// If both players have played, determine the winner
	if gameSession.HasPlayer1Played() && gameSession.HasPlayer2Played() {
		log.Printf(Green+"[INFO] Both players have played | SessionID: %s | Round: %d"+Reset, gameSession.SessionID, gameSession.CurrentRound)
		determineWinner(gameSession)

		// Log before incrementing the round
		log.Printf(Green+"[INFO] Incrementing round | SessionID: %s | Current Round: %d"+Reset, gameSession.SessionID, gameSession.CurrentRound)

		// Prepare for the next round
		gameSession.CurrentRound++
		gameSession.Results = append(gameSession.Results, models.RoundResult{
			RoundNumber: gameSession.CurrentRound,
		})
		gameSession.SetPlayer1HasPlayed(false)
		gameSession.SetPlayer2HasPlayed(false)
	}

	// Publish the updated game session to Kafka
	if err := kafka.UpdateSession(topicName, gameSession, kafkaBroker); err != nil {
		log.Printf(Orange+"[ERROR] Error updating session | SessionID: %s | Error: %v"+Reset, gameSession.SessionID, err)
	}

	return nil
}

// determineWinner determines the winner of the current round and updates the game session accordingly
func determineWinner(session *models.GameSession) {
	currentRound := &session.Results[session.CurrentRound-1]
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
	log.Println(Green + "[INFO] Received request to StatsHandler" + Reset)

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
