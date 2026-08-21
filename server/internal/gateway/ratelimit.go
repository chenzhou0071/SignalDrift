// ratelimit.go — 限流：令牌桶 + 每 IP 独立桶（懒创建、只建不删，见博客风险说明）
package gateway

import (
	"sync"
	"time"
)

type TokenBucket struct {
	mu     sync.Mutex
	tokens float64
	rate   float64 // 每秒补充
	burst  float64 // 桶容量
	last   time.Time
	now    func() time.Time
}

func NewTokenBucket(rate, burst float64, now func() time.Time) *TokenBucket {
	if now == nil {
		now = time.Now
	}
	return &TokenBucket{tokens: burst, rate: rate, burst: burst, last: now(), now: now}
}

func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := b.now()
	b.tokens += n.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = n
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type IPLimiter struct {
	mu      sync.Mutex
	buckets map[string]*TokenBucket
	rate    float64
	burst   float64
	now     func() time.Time
}

func NewIPLimiter(rate, burst float64, now func() time.Time) *IPLimiter {
	return &IPLimiter{buckets: make(map[string]*TokenBucket), rate: rate, burst: burst, now: now}
}

func (l *IPLimiter) Allow(ip string) bool {
	l.mu.Lock()
	b, ok := l.buckets[ip]
	if !ok {
		b = NewTokenBucket(l.rate, l.burst, l.now)
		l.buckets[ip] = b
	}
	l.mu.Unlock()
	return b.Allow()
}
