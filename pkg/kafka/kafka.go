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

// CreateKafkaTopic creates a topic
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

// MonitorKafkaAvailability ensure that topic is available
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

			for _, topic := range topics {
				err := CreateKafkaTopic(brokers, topic, partitions, replicationFactor)
				if err != nil && err != kafka.TopicAlreadyExists {
					log.Printf("Failed to create Kafka topic %s: %v", topic, err)
				} else {
					log.Printf("Topic %s is available.", topic)
				}
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

func FetchGameSession(sessionID string, kafkaBroker string, prefix string) (*models.GameSession, kafka.Message, *kafka.Reader, error) {
	// Build different consumer group for client/server per sessionID
	groupID := prefix + "-game-session-group-" + sessionID
	sessionTopic := "game-results-" + sessionID
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{kafkaBroker},
		Topic:          sessionTopic,
		GroupID:        groupID,
		MinBytes:       1,                     // Fetch immediately
		MaxBytes:       1e6,                   // 1MB max fetch size
		MaxWait:        10 * time.Millisecond, // Wait 10ms max
		CommitInterval: 0,                     // Disable auto-commit to handle commits manually
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg, err := reader.FetchMessage(ctx)
	if err != nil {
		reader.Close()
		if errors.Is(err, context.DeadlineExceeded) {
			log.Printf(Yellow+"[INFO] No message found within timeout for topic %s"+Reset, sessionTopic)
			return nil, kafka.Message{}, nil, nil // No message found within the timeout
		}
		log.Printf(Red+"[ERROR] Error fetching message: %v"+Reset, err)
		return nil, kafka.Message{}, nil, fmt.Errorf("error fetching message: %w", err)
	}

	log.Printf(Green+"[INFO] Message fetched from Kafka: %s in topic %s"+Reset, string(msg.Value), sessionTopic) // Log the entire message

	var gameSession models.GameSession
	if err := json.Unmarshal(msg.Value, &gameSession); err != nil {
		reader.Close()
		log.Printf(Red+"[ERROR] Error unmarshalling message: %v"+Reset, err)
		return nil, kafka.Message{}, nil, fmt.Errorf("error unmarshalling message: %w", err)
	}

	log.Printf(Green+"[INFO] Unmarshalled GameSession: %+v"+Reset, gameSession) // Log the unmarshalled session

	// Ensure the message is for the correct session
	if gameSession.SessionID == sessionID {
		log.Printf(Green+"[INFO] Game session found: %s"+Reset, gameSession.SessionID)
		return &gameSession, msg, reader, nil
	}

	log.Printf(Yellow+"[INFO] Session ID mismatch: %s vs %s"+Reset, sessionID, gameSession.SessionID)
	reader.Close()
	return nil, kafka.Message{}, nil, nil
}

// ReadGameSession reads a GameSession from Kafka based on the sessionID.
func ReadGameSession(topic string, sessionID string, kafkaBroker string, prefix string) (*models.GameSession, error) {
	// Build different consumer group for client/server per sessionID
	groupID := prefix + "-game-session-group-" + sessionID
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{kafkaBroker},
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1,                      // Fetch immediately
		MaxBytes:       1e6,                    // 1MB max fetch size
		MaxWait:        10 * time.Millisecond,  // Wait 10ms max
		CommitInterval: 100 * time.Millisecond, // Commit offsets frequently
	})

	defer reader.Close()

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

// updateSession publishes the updated game session to the Kafka game-results topic
func UpdateSession(topic string, session *models.GameSession, kafkaBroker string) error {
	log.Printf(Orange+"[INFO] Updating session | SessionID: %s"+Reset, session.SessionID)

	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers:      []string{kafkaBroker},
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    1,                    // Send each message individually
		BatchBytes:   1e6,                  // 1MB max batch size
		BatchTimeout: 5 * time.Millisecond, // Send immediately
	})
	defer writer.Close()

	sessionBytes, err := json.Marshal(session)
	if err != nil {
		log.Printf(Red+"[ERROR] Failed to marshal session | SessionID: %s | Error: %v"+Reset, session.SessionID, err)
		return err
	}

	if err := writer.WriteMessages(context.Background(), kafka.Message{
		Key:   []byte(session.SessionID),
		Value: sessionBytes,
	}); err != nil {
		log.Printf(Orange+"[ERROR] Failed to write message to Kafka topic %s | SessionID: %s | Error: %v"+Reset, topic, session.SessionID, err)
		return err
	}
	log.Printf(Orange+"[INFO] Successfully wrote session update to Kafka topic | SessionID: %s"+Reset, session.SessionID)
	log.Printf(Green+"[INFO] Message written to Kafka topic %s: %s"+Reset, topic, sessionBytes) // Log the entire message

	return nil
}

// Function to create a Kafka topic
func CreateTopicForSession(kafkaBroker string, sessionID string, partitions int, replicationFactor int) error {
	topicName := "game-results-" + sessionID
	return CreateKafkaTopic([]string{kafkaBroker}, topicName, partitions, replicationFactor)
}
