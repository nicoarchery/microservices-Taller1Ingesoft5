package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
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
	rand.Seed(time.Now().UnixNano())

	db := openDatabase()
	defer db.Close()

	pingDatabase(db)

	store := storedb.NewStore(db)
	if err := store.Prepare(); err != nil {
		log.Panic(err)
	}

	master := getKafkaMaster()
	defer master.Close()

	consumer, err := master.ConsumePartition(*topic, 0, sarama.OffsetOldest)
	if err != nil {
		log.Panic(err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	doneCh := make(chan struct{})

	go func() {
		for {
			select {
			case err := <-consumer.Errors():
				log.Println(err)
			case msg := <-consumer.Messages():
				*messageCountStart++
				log.Printf("Received message: user %s vote %s", string(msg.Key), string(msg.Value))
				persistVote(store, *messageCountStart, string(msg.Value))
			case <-signals:
				log.Println("Interrupt is detected")
				doneCh <- struct{}{}
			}
		}
	}()

	<-doneCh
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

func getKafkaMaster() sarama.Consumer {
	kingpin.Parse()
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	brokers := *brokerList
	fmt.Println("Waiting for kafka...")
	for {
		master, err := sarama.NewConsumer(brokers, config)
		if err == nil {
			fmt.Println("Kafka connected!")
			return master
		}
	}
}
