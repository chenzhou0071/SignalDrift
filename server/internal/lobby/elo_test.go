package lobby

import "testing"

func TestEloEqualRating(t *testing.T) {
	w, l := NewElo(1000, 1000, 32)
	if w != 1016 || l != 984 {
		t.Fatalf("w=%d l=%d", w, l)
	}
}

func TestEloUnderdogWin(t *testing.T) {
	w, l := NewElo(1000, 1200, 32)
	if w <= 1016 { // 爆冷夺分更多
		t.Fatalf("w=%d", w)
	}
	if w-1000 != 1200-l {
		t.Fatal("zero-sum violated")
	}
}

func TestEloDraw(t *testing.T) {
	a, b := NewEloDraw(1000, 1200, 32)
	if a <= 1000 || b >= 1200 {
		t.Fatalf("a=%d b=%d", a, b)
	}
	if a+b != 1000+1200 {
		t.Fatalf("draw zero-sum violated: a=%d b=%d", a, b)
	}
}
