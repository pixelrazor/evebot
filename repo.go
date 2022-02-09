package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type DataRepository interface {
	// interface for dealing with muting users
	AddMuted(userID string, mutedUntil time.Time)
	DeleteMuted(userID string)
	GetMuted(userID string) (time.Time, error)
	GetAllMuted() map[string]time.Time

	// interface for deailing with server traffic
	IncrementJoin(month string)
	GetAllJoin() map[string]int
	IncrementLeave(month string)
	// TODO: merge the join/leave getters
	GetAllLeave() map[string]int
}

type MemoryRepo struct {
	join  map[string]int
	leave map[string]int
	muted map[string]time.Time
	sync.RWMutex
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{
		join:  make(map[string]int),
		leave: make(map[string]int),
		muted: make(map[string]time.Time),
	}
}

func (mr *MemoryRepo) AddMuted(userID string, mutedUntil time.Time) {
	mr.Lock()
	defer mr.Unlock()
	mr.muted[userID] = mutedUntil
}

func (mr *MemoryRepo) DeleteMuted(userID string) {
	mr.Lock()
	defer mr.Unlock()
	delete(mr.muted, userID)
	return
}

func (mr *MemoryRepo) GetMuted(userID string) (time.Time, error) {
	mr.RLock()
	defer mr.RUnlock()
	until, ok := mr.muted[userID]
	if !ok {
		return time.Time{}, errors.New("user not muted")
	}
	return until, nil
}

func (mr *MemoryRepo) GetAllMuted() map[string]time.Time {
	mr.RLock()
	defer mr.RUnlock()
	ret := make(map[string]time.Time)
	for k, v := range mr.muted {
		ret[k] = v
	}
	return ret
}

func (mr *MemoryRepo) IncrementJoin(month string) {
	mr.Lock()
	defer mr.Unlock()
	v := mr.join[month]
	mr.join[month] = v + 1
}

func (mr *MemoryRepo) GetAllJoin() map[string]int {
	mr.RLock()
	defer mr.RUnlock()
	ret := make(map[string]int)
	for k, v := range mr.join {
		ret[k] = v
	}
	return ret
}

func (mr *MemoryRepo) IncrementLeave(month string) {
	mr.Lock()
	defer mr.Unlock()
	v := mr.leave[month]
	mr.leave[month] = v + 1
}

func (mr *MemoryRepo) GetAllLeave() map[string]int {
	mr.RLock()
	defer mr.RUnlock()
	ret := make(map[string]int)
	for k, v := range mr.leave {
		ret[k] = v
	}
	return ret
}

type PostgresRepo struct {
	db *sql.DB
}

func NewPostgresRepo(host, dbname, user, password string) (*PostgresRepo, error) {
	db, err := sql.Open("postgres", fmt.Sprintf("host=%v dbname=%v user=%v password=%v sslmode=disable", host, dbname, user, password))
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &PostgresRepo{db: db}, nil
}

func (pr *PostgresRepo) Close() error {
	return pr.db.Close()
}

func (pr *PostgresRepo) AddMuted(userID string, mutedUntil time.Time) {
	_, err := pr.db.Exec("INSERT INTO muted (user_id, muted_until) VALUES ($1, $2) ON CONFLICT (user_id) DO UPDATE SET muted_until = EXCLUDED.muted_until", userID, mutedUntil)
	if err != nil {
		log.Println("Failed inserting muted user:", err)
	}
}

func (pr *PostgresRepo) DeleteMuted(userID string) {
	_, err := pr.db.Exec("DELETE FROM muted WHERE user_id = $1", userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Println("Failed to delete muted user:", err)
	}
}

func (pr *PostgresRepo) GetMuted(userID string) (time.Time, error) {
	var mutedUntil time.Time
	err := pr.db.QueryRow("SELECT muted_until FROM muted WHERE user_id = $1", userID).Scan(&mutedUntil)
	if err != nil {
		return time.Time{}, err
	}
	return mutedUntil, nil
}

func (pr *PostgresRepo) GetAllMuted() map[string]time.Time {
	mutedUsers := make(map[string]time.Time)
	rows, err := pr.db.Query("SELECT user_id, muted_until FROM muted")
	if err != nil {
		log.Println("Failed to get muted users:", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var mutedUntil time.Time
		err := rows.Scan(&id, &mutedUntil)
		if err != nil {
			log.Println("Failed scanning muted user data:", err)
			return mutedUsers
		}
		mutedUsers[id] = mutedUntil
	}

	if err := rows.Err(); err != nil {
		log.Println("Error during muted users row iteration:", err)
	}
	return mutedUsers
}

func (pr *PostgresRepo) IncrementJoin(month string) {
	_, err := pr.db.Exec("INSERT INTO monthly_stats (month_year, members_joined, members_left) VALUES ($1, 1, 0) ON CONFLICT (month_year) DO UPDATE SET members_joined = monthly_stats.members_joined + 1", month)
	if err != nil {
		log.Println("Failed to increment join count:", err)
	}
}

func (pr *PostgresRepo) GetAllJoin() map[string]int {
	joined := make(map[string]int)
	rows, err := pr.db.Query("SELECT month_year, members_joined FROM monthly_stats")
	if err != nil {
		log.Println("Failed to get join count:", err)
	}
	defer rows.Close()
	for rows.Next() {
		var month string
		var count int
		err := rows.Scan(&month, &count)
		if err != nil {
			log.Println("Failed scanning join count data:", err)
			return joined
		}
		joined[month] = count
	}

	if err := rows.Err(); err != nil {
		log.Println("Error during join count row iteration:", err)
	}
	return joined
}

func (pr *PostgresRepo) IncrementLeave(month string) {
	_, err := pr.db.Exec("INSERT INTO monthly_stats (month_year, members_joined, members_left) VALUES ($1, 0, 1) ON CONFLICT (month_year) DO UPDATE SET members_left = monthly_stats.members_left + 1", month)
	if err != nil {
		log.Println("Failed to increment leave count:", err)
	}
}

func (pr *PostgresRepo) GetAllLeave() map[string]int {
	leave := make(map[string]int)
	rows, err := pr.db.Query("SELECT month_year, members_left FROM monthly_stats")
	if err != nil {
		log.Println("Failed to get leave count:", err)
	}
	defer rows.Close()
	for rows.Next() {
		var month string
		var count int
		err := rows.Scan(&month, &count)
		if err != nil {
			log.Println("Failed scanning leave count data:", err)
			return leave
		}
		leave[month] = count
	}

	if err := rows.Err(); err != nil {
		log.Println("Error during leave count row iteration:", err)
	}
	return leave
}
