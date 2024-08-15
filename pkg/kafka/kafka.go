package kafka

import (
	"context"
	"log"
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
