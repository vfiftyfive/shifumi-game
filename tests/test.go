package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

func main() {
	kafkaBroker := os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		log.Fatal("KAFKA_BROKER environment variable is not set")
	}
	topic := "test-topic"
	groupID := "test-group"

	// Create Kafka topic if it doesn't exist
	err := createKafkaTopic(kafkaBroker, topic)
	if err != nil {
		log.Fatalf("Failed to create Kafka topic: %v", err)
	}

	// Write a test message to Kafka
	err = writeTestMessage(kafkaBroker, topic)
	if err != nil {
		log.Fatalf("Failed to write message to Kafka: %v", err)
	}

	// Read the test message from Kafka
	err = readTestMessage(kafkaBroker, topic, groupID)
	if err != nil {
		log.Fatalf("Failed to read message from Kafka: %v", err)
	}
}

func createKafkaTopic(broker, topic string) error {
	conn, err := kafka.DialLeader(context.Background(), "tcp", broker, topic, 0)
	if err != nil {
		return err
	}
	defer conn.Close()

	topicConfig := kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	}

	err = conn.CreateTopics(topicConfig)
	if err != nil && err != kafka.TopicAlreadyExists {
		return err
	}

	log.Printf("Topic %s created or already exists.", topic)
	return nil
}

func writeTestMessage(broker, topic string) error {
	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers: []string{broker},
		Topic:   topic,
	})
	defer writer.Close()

	message := kafka.Message{
		Key:   []byte("test-key"),
		Value: []byte("Hello Kafka"),
	}

	err := writer.WriteMessages(context.Background(), message)
	if err != nil {
		return err
	}

	log.Println("Successfully wrote message to Kafka")
	return nil
}

func readTestMessage(broker, topic, groupID string) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{broker},
		Topic:    topic,
		GroupID:  groupID,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
		MaxWait:  1 * time.Second,
	})
	defer reader.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg, err := reader.ReadMessage(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Received message: %s: %s\n", string(msg.Key), string(msg.Value))
	return nil
}
