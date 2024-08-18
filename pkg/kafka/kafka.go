package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"shifumi-game/pkg/models"
	"time"

	"github.com/segmentio/kafka-go"
)

const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Orange = "\033[33m"
)

// WriteMessage writes a message to Kafka using a given writer
func WriteMessages(writer *kafka.Writer, key, value []byte) error {
	err := writer.WriteMessages(context.Background(), kafka.Message{
		Key:   key,
		Value: value,
	})
	if err != nil {
		log.Printf("Failed to write message: %v", err)
		return err
	}
	return nil
}

// ReadMessages reads messages from Kafka using a given reader and processes them using the provided handler function
func ReadMessages(reader *kafka.Reader, handleMessage func(key, value []byte) error) error {
	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			return err
		}
		if err := handleMessage(msg.Key, msg.Value); err != nil {
			return err
		}
	}
}

// CreateKafkaTopic ensures the Kafka topic exists
func CreateKafkaTopic(brokers []string, topic string, partitions, replicationFactor int) error {
	conn, err := kafka.DialLeader(context.Background(), "tcp", brokers[0], topic, 0)
	if err != nil {
		return err
	}
	defer conn.Close()

	topicConfig := kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: replicationFactor,
	}

	err = conn.CreateTopics(topicConfig)
	if err != nil && err != kafka.TopicAlreadyExists {
		return err
	}

	log.Printf("Topic %s is available.", topic)
	return nil
}

func MonitorKafkaAvailability(kafkaBroker string, topics []string, partitions, replicationFactor int, interval time.Duration) {
	backoff := interval

	// Convert kafkaBroker to a slice of strings
	brokers := []string{kafkaBroker}

	for {
		for _, topic := range topics {
			err := CreateKafkaTopic(brokers, topic, partitions, replicationFactor)
			if err != nil && err != kafka.TopicAlreadyExists {
				log.Printf("Failed to create Kafka topic %s: %v", topic, err)
			} else {
				log.Printf("Topic %s is available.", topic)
			}
		}

		// Attempt to connect to Kafka
		writer := kafka.NewWriter(kafka.WriterConfig{
			Brokers:  brokers,
			Topic:    topics[0],
			Balancer: &kafka.LeastBytes{},
		})
		defer writer.Close()

		err := WriteMessages(writer, []byte("test-key"), []byte("test-value"))
		if err != nil {
			// Log the error message every `interval`
			log.Printf("Failed to connect to Kafka: %v", err)
		} else {
			// Successfully connected, exit the loop
			log.Println("Successfully connected to Kafka.")
			return
		}

		// Exponential backoff with a cap
		time.Sleep(backoff)
		if backoff < 2*time.Minute {
			backoff *= 2
		}
	}
}

// ReadGameSession reads a GameSession from Kafka based on the sessionID.
// It assumes you're NOT using a consumer group.
// The `offset` parameter controls which message to read:
//   - kafka.FirstOffset: Reads the first message in the topic.
//   - kafka.LastOffset:  Reads the last message in the topic.
func ReadGameSession(sessionID string, kafkaBroker string, offset int64) (*models.GameSession, error) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    "game-session",
		GroupID:  "game-session-reader",
		MinBytes: 1,    // Fetch even the smallest message
		MaxBytes: 10e6, // Allow fetching large messages
	})

	defer reader.Close()

	// Seek to the specified offset
	err := reader.SetOffset(offset)
	if err != nil {
		log.Printf(Red+"[ERROR] Failed to set Kafka offset: %v"+Reset, err)
		return nil, fmt.Errorf("failed to set offset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				log.Printf(Yellow + "[INFO] No message found within timeout" + Reset)
				return nil, nil // No message found within the timeout
			}
			log.Printf(Red+"[ERROR] Error reading message: %v"+Reset, err)
			return nil, fmt.Errorf("error reading message: %w", err)
		}

		log.Printf(Green+"[INFO] Message read from Kafka: %s"+Reset, string(msg.Value)) // Log the entire message

		var gameSession models.GameSession
		if err := json.Unmarshal(msg.Value, &gameSession); err != nil {
			log.Printf(Red+"[ERROR] Error unmarshalling message: %v"+Reset, err)
			return nil, fmt.Errorf("error unmarshalling message: %w", err)
		}

		log.Printf(Green+"[INFO] Unmarshalled GameSession: %+v"+Reset, gameSession) // Log the unmarshalled session

		// Ensure the message is for the correct session
		if gameSession.SessionID == sessionID {
			log.Printf(Green+"[INFO] Game session found: %s"+Reset, gameSession.SessionID)
			return &gameSession, nil
		}

		log.Printf(Yellow+"[INFO] Session ID mismatch: %s vs %s"+Reset, sessionID, gameSession.SessionID)
	}
}
