// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newPolicyRateLimiter(minimumSecondsPerItem uint32) workqueue.TypedRateLimiter[reconcile.Request] {
	return workqueue.NewTypedMaxOfRateLimiter(
		// Based on the one in client-go@v09.31.9 DefaultTypedControllerRateLimiter
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
			500*time.Millisecond, // base delay: 0.5 seconds (5ms in client-go's default)
			10*time.Minute,       // max delay: 10 minutes (16m40s in client-go's default)
		),
		// This is an overall (not per-item) limiter with 10 qps, 100 bucket size.
		// This is identical to the one in client-go@v09.31.9 DefaultTypedControllerRateLimiter
		&workqueue.TypedBucketRateLimiter[reconcile.Request]{
			Limiter: rate.NewLimiter(rate.Limit(10), 100),
		},
		// This limits each item individually, so each has a minimum interval between reconciles.
		&PerItemRateLimiter[reconcile.Request]{
			limiters: map[reconcile.Request]*rate.Limiter{},
			rate:     rate.Every(time.Second * time.Duration(minimumSecondsPerItem)),
			burst:    1,
		},
	)
}

type PerItemRateLimiter[T comparable] struct {
	lock     sync.Mutex
	limiters map[T]*rate.Limiter
	rate     rate.Limit
	burst    int
}

// Forget is a no-op for a PerItemRateLimiter. RateLimiters in client-go only limit retries on
// failures, but this limiter applies to *all* requests.
func (r *PerItemRateLimiter[T]) Forget(item T) {
}

// NumRequeues always returns 0 for a PerItemRateLimiter.
func (r *PerItemRateLimiter[T]) NumRequeues(item T) int {
	return 0
}

func (r *PerItemRateLimiter[T]) When(item T) time.Duration {
	r.lock.Lock()
	defer r.lock.Unlock()

	limiter, ok := r.limiters[item]
	if !ok {
		limiter = rate.NewLimiter(r.rate, r.burst)
		r.limiters[item] = limiter
	}

	return limiter.Reserve().Delay()
}
