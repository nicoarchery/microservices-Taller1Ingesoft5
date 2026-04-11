package db

import (
	"log"
	"time"

	"github.com/sony/gobreaker/v2"
)

type CircuitBreaker = gobreaker.CircuitBreaker[any]

func NewDBCircuitBreaker() *CircuitBreaker {
	settings := gobreaker.Settings{
		Name:        "PostgreSQL-CB",
		MaxRequests: 1,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			if counts.Requests < 5 {
				return false
			}

			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return failureRatio >= 0.5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Printf("CircuitBreaker %s: %s -> %s", name, from, to)
		},
	}

	return gobreaker.NewCircuitBreaker[any](settings)
}