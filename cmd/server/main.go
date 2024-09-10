package main

import (
	"log"
	"net/http"
	"os"
	api "shifumi-game/api/server"
	"shifumi-game/pkg/kafka"
	"time"
)

func main() {
	kafkaBroker := os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		log.Fatal("KAFKA_BROKER environment variable is not set")
	}

	// Topics to monitor
	topics := []string{"player-choices"}

	// Monitor Kafka availability before starting the server
	log.Println("[INFO] Waiting for Kafka to be available...")
	kafka.MonitorKafkaAvailability(kafkaBroker, topics, 1, 1)
	log.Println("[INFO] Kafka is available. Starting game logic service setup...")

	// Start processing player choices in a separate goroutine
	go func() {
		for {
			log.Println("[INFO] Attempting to start processing player choices...")
			api.ProcessChoices(kafkaBroker)
			log.Println("[WARN] ProcessChoices function exited unexpectedly. Restarting...")
			time.Sleep(2 * time.Second) // Sleep briefly before restarting to avoid tight loops in case of persistent errors
		}
	}()

	// Registering handlers for live stats
	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		api.StatsHandler(w, r, kafkaBroker)
	})

	log.Println("[INFO] Game logic service is running on port 8082")
	log.Fatal(http.ListenAndServe(":8082", nil)) // Serve on port 8082
}
