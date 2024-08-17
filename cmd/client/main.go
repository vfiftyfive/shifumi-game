package main

import (
	"log"
	"net/http"
	"os"
	api "shifumi-game/api/client"
)

func main() {
	kafkaBroker := os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		log.Fatal("KAFKA_BROKER environment variable is not set")
	}

	http.HandleFunc("/play", func(w http.ResponseWriter, r *http.Request) {
		api.MakeChoiceHandler(w, r, kafkaBroker)
	})
	log.Fatal(http.ListenAndServe(":8081", nil))
}
