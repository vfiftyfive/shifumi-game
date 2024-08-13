package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"shifumi-game/pkg/kafka"
	"shifumi-game/pkg/models"
	"sync"
	"time"
)

var (
	sessions = make(map[string]*models.GameSession)
	mu       sync.Mutex
)

func ProcessChoices(kafkaBroker string) {
	reader := kafka.NewKafkaReader([]string{kafkaBroker}, "player-choices", "game-logic")
	defer reader.Close()

	err := kafka.ReadMessages(reader, func(key, value []byte) error {
		return handlePlayerChoice(key, value, kafkaBroker)
	})
	if err != nil {
		log.Printf("Error reading messages: %v", err)
	}
}

func handlePlayerChoice(key, value []byte, kafkaBroker string) error {
	var choice models.PlayerChoice
	if err := json.Unmarshal(value, &choice); err != nil {
		log.Printf("Error unmarshalling player choice: %v", err)
		return err
	}

	if !isValidChoice(choice.Choice) {
		log.Printf("Invalid choice received: %s", choice.Choice)
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	session, exists := sessions[choice.SessionID]
	if !exists {
		session = &models.GameSession{
			SessionID: choice.SessionID,
			Status:    "in progress",
		}
		sessions[choice.SessionID] = session
	}

	if session.Player1 == nil {
		choice.PlayerID = 1
		session.Player1 = &choice
	} else if session.Player2 == nil {
		choice.PlayerID = 2
		session.Player2 = &choice
	} else {
		// Ignore additional players
		log.Printf("Session %s already has two players", choice.SessionID)
		return nil
	}

	if session.Player1 != nil && session.Player2 != nil {
		determineWinner(session, kafkaBroker)
	} else {
		updateSession(session, kafkaBroker)
	}

	return nil
}

func updateSession(session *models.GameSession, kafkaBroker string) {
	// Send an update message to Kafka to signal that the session is still ongoing
	result := models.GameResult{
		SessionID:   session.SessionID,
		Player1Wins: session.Player1Wins,
		Player2Wins: session.Player2Wins,
		Draws:       session.Draws,
		Status:      session.Status,
		Message:     "Waiting for both players",
	}

	writer := kafka.NewKafkaWriter([]string{kafkaBroker}, "game-results")
	defer writer.Close()

	resultBytes, _ := json.Marshal(result)
	kafka.WriteMessage(writer, []byte(session.SessionID), resultBytes)
}

func determineWinner(session *models.GameSession, kafkaBroker string) {
	var resultMessage string

	switch session.Player1.Choice {
	case "rock":
		switch session.Player2.Choice {
		case "scissors":
			session.Player1Wins++
			resultMessage = "Player 1 wins!"
		case "paper":
			session.Player2Wins++
			resultMessage = "Player 2 wins!"
		case "rock":
			session.Draws++
			resultMessage = "It's a draw!"
		}
	case "paper":
		switch session.Player2.Choice {
		case "rock":
			session.Player1Wins++
			resultMessage = "Player 1 wins!"
		case "scissors":
			session.Player2Wins++
			resultMessage = "Player 2 wins!"
		case "paper":
			session.Draws++
			resultMessage = "It's a draw!"
		}
	case "scissors":
		switch session.Player2.Choice {
		case "paper":
			session.Player1Wins++
			resultMessage = "Player 1 wins!"
		case "rock":
			session.Player2Wins++
			resultMessage = "Player 2 wins!"
		case "scissors":
			session.Draws++
			resultMessage = "It's a draw!"
		}
	}

	if session.Player1Wins == 3 {
		session.Status = "Player 1 wins the game!"
		resultMessage = "Player 1 wins the game!"
	} else if session.Player2Wins == 3 {
		session.Status = "Player 2 wins the game!"
		resultMessage = "Player 2 wins the game!"
	} else {
		session.Status = "in progress"
	}

	result := models.GameResult{
		SessionID:   session.SessionID,
		Player1Wins: session.Player1Wins,
		Player2Wins: session.Player2Wins,
		Draws:       session.Draws,
		Status:      session.Status,
		Message:     resultMessage,
		Player1:     session.Player1.Choice,
		Player2:     session.Player2.Choice,
	}

	writer := kafka.NewKafkaWriter([]string{kafkaBroker}, "game-results")
	defer writer.Close()

	resultBytes, _ := json.Marshal(result)
	kafka.WriteMessage(writer, []byte(session.SessionID), resultBytes)
}

func GetPlayerStatsHandler(w http.ResponseWriter, r *http.Request, kafkaBroker string) {
	// Set a timeout context for reading from Kafka
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reader := kafka.NewKafkaReader([]string{kafkaBroker}, "game-results", "player")
	defer reader.Close()

	var results []models.GameResult

	// Attempt to fetch a message with the provided context
	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if err == context.DeadlineExceeded {
				http.Error(w, "No game results available yet", http.StatusNoContent)
				return
			}
			log.Printf("Error fetching message from Kafka: %v", err)
			http.Error(w, "Error retrieving player stats: "+err.Error(), http.StatusInternalServerError)
			return
		}

		var result models.GameResult
		if err := json.Unmarshal(msg.Value, &result); err != nil {
			log.Printf("Error unmarshalling game result: %v", err)
			http.Error(w, "Error retrieving player stats: "+err.Error(), http.StatusInternalServerError)
			return
		}
		results = append(results, result)

		// Break the loop after retrieving the first message to avoid hanging
		break
	}

	// Return the collected game stats as JSON
	w.Header().Set("Content-Type", "application/json")
	if len(results) == 0 {
		w.Write([]byte("[]"))
	} else {
		json.NewEncoder(w).Encode(results)
	}
}
