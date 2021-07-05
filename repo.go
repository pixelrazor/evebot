package main

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

var ctx = context.Background()

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
	GetAllLeave() map[string]int
}

type RedisRepo struct {
	db *redis.Client
}

func NewRedisRepo(db *redis.Client) *RedisRepo {
	return &RedisRepo{db: db}
}

func (rr *RedisRepo) AddMuted(userID string, mutedUntil time.Time) {
	err := rr.db.Set(ctx, "muted:"+userID, mutedUntil.Format(time.RFC3339), 0).Err()
	if err != nil {
		log.Println("Redis set muted error:", userID, err)
	}
}

func (rr *RedisRepo) DeleteMuted(userID string) {
	err := rr.db.Del(ctx, "muted:"+userID).Err()
	if err != nil {
		log.Println("Redis del muted error:", userID, err)
	}
}

func (rr *RedisRepo) GetMuted(userID string) (time.Time, error) {
	res, err := rr.db.Get(ctx, "muted:"+userID).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Println("Redis get muted user error:", userID, err)
		}
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, res)
}

func (rr *RedisRepo) GetAllMuted() map[string]time.Time {
	keys, err := rr.db.Keys(ctx, "muted:*").Result()
	if err != nil {
		log.Println("Redis muted keys error:", err)
		return nil
	}
	vals := make(map[string]time.Time)
	for _, key := range keys {
		val, err := rr.db.Get(ctx, key).Result()
		if err != nil {
			log.Println("Redis get muted error:", key, err)
			return nil
		}
		t, _ := time.Parse(time.RFC3339, val)
		vals[strings.TrimPrefix(key, "muted:")] = t
	}
	return vals
}

func (rr *RedisRepo) IncrementJoin(month string) {
	err := rr.db.Incr(ctx, "join:"+month).Err()
	if err != nil {
		log.Println("Redis incr join error:", err)
	}
}

func (rr *RedisRepo) GetAllJoin() map[string]int {
	keys, err := rr.db.Keys(ctx, "join:*").Result()
	if err != nil {
		log.Println("Redis join keys error:", err)
		return nil
	}
	vals := make(map[string]int)
	for _, key := range keys {
		val, err := rr.db.Get(ctx, key).Result()
		if err != nil {
			log.Println("Redis get join error:", key, err)
			return nil
		}
		i, _ := strconv.Atoi(val)
		vals[strings.TrimPrefix(key, "join:")] = i
	}
	return vals
}

func (rr *RedisRepo) IncrementLeave(month string) {
	err := rr.db.Incr(ctx, "leave:"+month).Err()
	if err != nil {
		log.Println("Redis incr join error:", err)
	}
}

func (rr *RedisRepo) GetAllLeave() map[string]int {
	keys, err := rr.db.Keys(ctx, "leave:*").Result()
	if err != nil {
		log.Println("Redis leave keys error:", err)
		return nil
	}
	vals := make(map[string]int)
	for _, key := range keys {
		val, err := rr.db.Get(ctx, key).Result()
		if err != nil {
			log.Println("Redis get leave error:", key, err)
			return nil
		}
		i, _ := strconv.Atoi(val)
		vals[strings.TrimPrefix(key, "leave:")] = i
	}
	return vals
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
