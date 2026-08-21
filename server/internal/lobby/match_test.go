// match_test.go — 匹配池测试：池内/超差/放宽/取消/重复/时钟回拨
package lobby

import (
	"testing"

	"signaldrift/server/internal/config"
)

func testPool(now *int64) *MatchPool {
	return NewMatchPool(config.MatchConfig{BaseGap: 100, GapStep: 100, WidenSec: 30},
		func() int64 { return *now })
}

func TestMatchWithinGap(t *testing.T) {
	now := int64(0)
	p := testPool(&now)
	p.Add(1, 1000)
	p.Add(2, 1080)
	pairs := p.Poll()
	if len(pairs) != 1 || p.Size() != 0 {
		t.Fatalf("pairs=%v size=%d", pairs, p.Size())
	}
}

func TestNoMatchOverGap(t *testing.T) {
	now := int64(0)
	p := testPool(&now)
	p.Add(1, 1000)
	p.Add(2, 1300)
	if pairs := p.Poll(); len(pairs) != 0 {
		t.Fatalf("pairs=%v", pairs)
	}
}

func TestWidenAfterWait(t *testing.T) {
	now := int64(0)
	p := testPool(&now)
	p.Add(1, 1000)
	p.Add(2, 1300)
	now = 65 // 两次放宽：允许分差 100+2*100=300
	pairs := p.Poll()
	if len(pairs) != 1 {
		t.Fatalf("pairs=%v", pairs)
	}
}

func TestCancel(t *testing.T) {
	now := int64(0)
	p := testPool(&now)
	p.Add(1, 1000)
	if !p.Cancel(1) || p.Size() != 0 {
		t.Fatal("cancel failed")
	}
	if p.Cancel(1) {
		t.Fatal("double cancel must return false")
	}
}

func TestDuplicateAdd(t *testing.T) {
	now := int64(0)
	p := testPool(&now)
	if !p.Add(1, 1000) || p.Add(1, 1000) {
		t.Fatal("duplicate add must return false")
	}
}

func TestClockRewindNoStrictGap(t *testing.T) {
	now := int64(100)
	p := testPool(&now)
	p.Add(1, 1000)
	p.Add(2, 1080)
	now = 50 // 时钟回拨：等待时间为负，放宽次数不得为负（分差不收紧）
	pairs := p.Poll()
	if len(pairs) != 1 {
		t.Fatalf("rewind must not tighten gap, pairs=%v", pairs)
	}
}
