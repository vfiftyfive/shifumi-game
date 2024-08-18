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
		MinBytes: 1,    // Fetch even the smallest message
		MaxBytes: 10e6, // Allow fetching large messages
	})

	defer reader.Close()

	// Seek to the specified offset
	err := reader.SetOffset(offset)
	if err != nil {
		return nil, fmt.Errorf("failed to set offset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, nil // No message found within the timeout
			}
			return nil, fmt.Errorf("error reading message: %w", err)
		}

		var gameSession models.GameSession
		if err := json.Unmarshal(msg.Value, &gameSession); err != nil {
			return nil, fmt.Errorf("error unmarshalling message: %w", err)
		}

		// Ensure the message is for the correct session
		if gameSession.SessionID == sessionID {
			return &gameSession, nil
		}

		// If we've reached the beginning of the partition and haven't found the session,
		// it likely doesn't exist.
		if msg.Offset == 0 {
			return nil, nil
		}
	}
}
