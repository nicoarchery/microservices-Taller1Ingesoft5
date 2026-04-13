package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	_ "github.com/lib/pq"

	kingpin "github.com/alecthomas/kingpin/v2"

	"github.com/IBM/sarama"
	storedb "github.com/okteto/microservicees-demo/worker/internal/db"
)

var (
	databaseInitialRetryDelay = durationFromEnv("DB_RETRY_INITIAL", 1*time.Second)
	databaseMaxRetryDelay     = durationFromEnv("DB_RETRY_MAX", 30*time.Second)
	retryJitterFactor         = floatFromEnv("DB_RETRY_JITTER", 0.2)

	brokerList        = kingpin.Flag("brokerList", "List of brokers to connect").Default("kafka:9092").Strings()
	topic             = kingpin.Flag("topic", "Topic name").Default("votes").String()
	consumerGroup     = kingpin.Flag("consumerGroup", "Consumer group ID").Default("vote-workers").String()
	messageCountStart = kingpin.Flag("messageCountStart", "Message counter start from:").Int()
)

const (
	host     = "postgresql"
	port     = 5432
	user     = "okteto"
	password = "okteto"
	dbname   = "votes"
)

func main() {
	kingpin.Parse()

	db := openDatabase()
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}()
//kkkkk
	pingDatabase(db)

	store := storedb.NewStore(db)
	if err := store.Prepare(); err != nil {
		log.Panic(err)
	}

	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	config.Consumer.Offsets.Initial = sarama.OffsetOldest

	brokers := *brokerList
	groupID := *consumerGroup
	topicName := *topic

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	handler := &VoteHandler{
		store:     store,
		msgCounter: messageCountStart,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for {
			consumerGroup, err := sarama.NewConsumerGroup(brokers, groupID, config)
			if err != nil {
				log.Printf("Error creating consumer group: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			log.Printf("Connected to Kafka as consumer group: %s", groupID)

			consumerErrors := consumerGroup.Errors()
			done := make(chan struct{})

			go func() {
				for {
					select {
					case err := <-consumerErrors:
						if err != nil {
							log.Printf("Consumer error: %v", err)
						}
					case <-done:
						return
					}
				}
			}()

			err = consumerGroup.Consume(ctx, []string{topicName}, handler)
			close(done)
			if err := consumerGroup.Close(); err != nil {
				log.Printf("error closing consumer group: %v", err)
			}

			if err != nil {
				log.Printf("Error from consumer: %v", err)
			}

			if ctx.Err() != nil {
				return
			}

			log.Println("Rebalancing or reconnecting...")
			time.Sleep(2 * time.Second)
		}
	}()

	<-signals
	log.Println("Interrupt detected, shutting down...")
	cancel()
	wg.Wait()
	log.Println("Processed", *messageCountStart, "messages")
}

func persistVote(store *storedb.Store, id int, vote string) {
	retryDelay := databaseInitialRetryDelay

	for {
		err := store.SaveVote(id, vote)
		if err == nil {
			return
		}

		if errors.Is(err, storedb.ErrCircuitOpen) {
			sleep := jitterDelay(retryDelay)
			log.Printf("database circuit open; retrying vote %d in %s", id, sleep)
			time.Sleep(sleep)
			retryDelay = nextRetryDelay(retryDelay)
			continue
		}

		sleep := jitterDelay(retryDelay)
		log.Printf("failed to persist vote %d: %v; retrying in %s", id, err, sleep)
		time.Sleep(sleep)
		retryDelay = nextRetryDelay(retryDelay)
	}
}

// VoteHandler implements sarama.ConsumerGroupHandler for processing votes
type VoteHandler struct {
	store      *storedb.Store
	msgCounter *int
}

func (h *VoteHandler) Setup(sarama.ConsumerGroupSession) error {
	log.Println("Consumer group session started")
	return nil
}

func (h *VoteHandler) Cleanup(sarama.ConsumerGroupSession) error {
	log.Println("Consumer group session ended")
	return nil
}

func (h *VoteHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		*h.msgCounter++
		log.Printf("[Worker] Received message: partition=%d offset=%d key=%s vote=%s",
			msg.Partition, msg.Offset, string(msg.Key), string(msg.Value))

		persistVote(h.store, *h.msgCounter, string(msg.Value))
		session.MarkMessage(msg, "")
	}
	return nil
}

func nextRetryDelay(current time.Duration) time.Duration {
	next := current * 2
	if next > databaseMaxRetryDelay {
		return databaseMaxRetryDelay
	}

	return next
}

func jitterDelay(base time.Duration) time.Duration {
	factor := clampJitter(retryJitterFactor)
	jitter := 1 + ((rand.Float64()*2 - 1) * factor)
	value := time.Duration(float64(base) * jitter)
	if value < databaseInitialRetryDelay {
		return databaseInitialRetryDelay
	}

	return value
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		log.Printf("invalid %s=%q, using default %s", name, raw, fallback)
		return fallback
	}

	return parsed
}

func floatFromEnv(name string, fallback float64) float64 {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		log.Printf("invalid %s=%q, using default %v", name, raw, fallback)
		return fallback
	}

	return parsed
}

func clampJitter(value float64) float64 {
	if value < 0 {
		return 0
	}

	if value > 1 {
		return 1
	}

	return value
}

func openDatabase() *sql.DB {
	psqlconn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", host, port, user, password, dbname)
	for {
		db, err := sql.Open("postgres", psqlconn)
		if err == nil {
			return db
		}
	}
}

func pingDatabase(db *sql.DB) {
	fmt.Println("Waiting for postgresql...")
	for {
		if err := db.Ping(); err == nil {
			fmt.Println("Postgresql connected!")
			return
		}
		time.Sleep(1 * time.Second)
	}
}
