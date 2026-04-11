package db

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/sony/gobreaker/v2"
)

var ErrCircuitOpen = errors.New("database circuit breaker is open")

type Store struct {
	db      *sql.DB
	breaker *CircuitBreaker
}

func NewStore(db *sql.DB) *Store {
	return &Store{
		db:      db,
		breaker: NewDBCircuitBreaker(),
	}
}

func (s *Store) Prepare() error {
	dropTableStmt := `DROP TABLE IF EXISTS votes`
	if _, err := s.db.Exec(dropTableStmt); err != nil {
		return fmt.Errorf("drop votes table: %w", err)
	}

	createTableStmt := `CREATE TABLE IF NOT EXISTS votes (id VARCHAR(255) NOT NULL UNIQUE, vote VARCHAR(255) NOT NULL)`
	if _, err := s.db.Exec(createTableStmt); err != nil {
		return fmt.Errorf("create votes table: %w", err)
	}

	return nil
}

func (s *Store) SaveVote(id int, vote string) error {
	_, err := s.breaker.Execute(func() (any, error) {
		stmt := `insert into "votes"("id", "vote") values($1, $2) on conflict(id) do update set vote = $2`
		if _, execErr := s.db.Exec(stmt, id, vote); execErr != nil {
			return nil, execErr
		}

		return nil, nil
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) {
			return ErrCircuitOpen
		}

		return fmt.Errorf("save vote: %w", err)
	}

	return nil
}