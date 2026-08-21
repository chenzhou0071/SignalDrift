// store.go — 存储抽象：Store 接口（10 方法）、领域结构体、哨兵错误（ErrDuplicate/ErrNotFound）
package store

import "errors"

type User struct {
	UID          int64
	Username     string
	Nickname     string
	PasswordHash string
}

type Profile struct {
	UID      int64
	Nickname string
	Elo      int
	MaxElo   int
	Wins     int
	Losses   int
}

type MatchRecord struct {
	UID                 int64
	Result              int // 1胜 0平 -1负
	EloChange           int
	FinalCoverage       float64
	PaintedCells        int
	StraightShots       int
	LobShots            int
	HitsOnEnemy         int
	BlackholeDestroyed  int
	ReflectCnt          int
	MatchDuration       int
}

var (
	ErrDuplicate = errors.New("store: duplicate")
	ErrNotFound  = errors.New("store: not found")
)

type Store interface {
	CreateUser(username, passwordHash string) (int64, error) // 重名返回 ErrDuplicate；nickname 初始为空
	GetUserByName(username string) (*User, error)
	GetUser(uid int64) (*User, error)                  // 按 UID 解析（含 nickname），不存在 ErrNotFound
	SetNickname(uid int64, nickname string) error      // 玩家自行设置/修改显示名
	GetProfile(uid int64) (*Profile, error)            // 不存在则返回初始档案(Elo 1000)并落库；Nickname 由 user 表填充
	UpdateElo(uid int64, newElo int, result int) error // result:1胜/0平/-1负；同步更新 MaxElo/Wins/Losses
	AddFriend(uid, friendUID int64) error
	DelFriend(uid, friendUID int64) error
	ListFriends(uid int64) ([]int64, error)
	InsertMatchRecord(r *MatchRecord) error
}
