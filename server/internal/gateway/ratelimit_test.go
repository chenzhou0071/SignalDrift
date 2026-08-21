// ratelimit_test.go — 限流测试：假时钟验证桶恢复/超限拒绝/IP 隔离
package gateway

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestTokenBucketBurstThenReject(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	b := NewTokenBucket(10, 3, clk.now) // 每秒10个，突发3
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("burst %d rejected", i)
		}
	}
	if b.Allow() {
		t.Fatal("4th should be rejected")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	b := NewTokenBucket(10, 3, clk.now)
	for i := 0; i < 3; i++ {
		b.Allow()
	}
	clk.advance(200 * time.Millisecond) // 补 2 个令牌
	if !b.Allow() || !b.Allow() {
		t.Fatal("refilled tokens should pass")
	}
	if b.Allow() {
		t.Fatal("over refill should be rejected")
	}
}

func TestIPLimiterIsolated(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := NewIPLimiter(10, 1, clk.now)
	if !l.Allow("1.1.1.1") {
		t.Fatal("first ip1")
	}
	if l.Allow("1.1.1.1") {
		t.Fatal("ip1 over burst")
	}
	if !l.Allow("2.2.2.2") {
		t.Fatal("ip2 must be isolated from ip1")
	}
}
