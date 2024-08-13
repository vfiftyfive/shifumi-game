package main

import (
	"log"
	"net/http"
	"os"
	"shifumi-game/api"
	"shifumi-game/pkg/kafka"
	"time"
)

func main() {
	// Retrieve the Kafka broker address from the environment variable
	kafkaBroker := os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		log.Fatal("KAFKA_BROKER environment variable is not set")
	}

	// Define the Kafka topics to monitor and ensure they exist
	topics := []string{"player-choices", "game-results"}

	// Monitor Kafka availability and ensure required topics exist
	go kafka.MonitorKafkaAvailability(kafkaBroker, topics, 1, 1, 10*time.Second)

	// Start processing player choices
	go api.ProcessChoices(kafkaBroker)

	// Set up the /stats endpoint to retrieve game statistics
	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		api.GetPlayerStatsHandler(w, r, kafkaBroker)
	})

	// Start the HTTP server on port 8082
	log.Println("Starting server on :8082")
	log.Fatal(http.ListenAndServe(":8082", nil))
}
