// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

func NewPerItemRateLimiter[T comparable](backoffSeconds uint32, burst int) *PerItemRateLimiter[T] {
	return &PerItemRateLimiter[T]{
		lock:     sync.RWMutex{},
		limiters: map[T]*rate.Limiter{},
		rate:     rate.Every(time.Second * time.Duration(backoffSeconds)),
		burst:    burst,
	}
}

type PerItemRateLimiter[T comparable] struct {
	lock     sync.RWMutex
	limiters map[T]*rate.Limiter
	rate     rate.Limit
	burst    int
}

func (l *PerItemRateLimiter[T]) GetLimiter(item T) *rate.Limiter {
	l.lock.RLock()
	limiter, exists := l.limiters[item]
	l.lock.RUnlock()

	if !exists {
		l.lock.Lock()

		limiter = rate.NewLimiter(l.rate, l.burst)
		l.limiters[item] = limiter

		l.lock.Unlock()
	}

	return limiter
}
