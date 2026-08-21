// elo.go — ELO 计算：标准公式（分差 400 基准），胜者零和、平局按期望差调整
package lobby

import "math"

func expected(a, b int) float64 {
	return 1.0 / (1.0 + math.Pow(10, float64(b-a)/400.0))
}

// NewElo 胜者/败者新分（四舍五入，零和）
func NewElo(winner, loser int, k float64) (int, int) {
	delta := int(math.Round(k * (1 - expected(winner, loser))))
	return winner + delta, loser - delta
}

// NewEloDraw 平局：各按期望差调整
func NewEloDraw(a, b int, k float64) (int, int) {
	delta := int(math.Round(k * (0.5 - expected(a, b))))
	return a + delta, b - delta
}
