package store

import (
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"
)

type MySQLStore struct{ db *sql.DB }

func NewMySQL(dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	return &MySQLStore{db: db}, nil
}

func isDup(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func (s *MySQLStore) CreateUser(username, hash string) (int64, error) {
	res, err := s.db.Exec("INSERT INTO user(username,password_hash) VALUES(?,?)", username, hash)
	if err != nil {
		if isDup(err) {
			return 0, ErrDuplicate
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) GetUserByName(username string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow("SELECT uid,username,nickname,password_hash FROM user WHERE username=?", username).
		Scan(&u.UID, &u.Username, &u.Nickname, &u.PasswordHash)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *MySQLStore) GetUser(uid int64) (*User, error) {
	u := &User{}
	err := s.db.QueryRow("SELECT uid,username,nickname,password_hash FROM user WHERE uid=?", uid).
		Scan(&u.UID, &u.Username, &u.Nickname, &u.PasswordHash)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *MySQLStore) SetNickname(uid int64, nickname string) error {
	res, err := s.db.Exec("UPDATE user SET nickname=? WHERE uid=?", nickname, uid)
	if err != nil {
		return err
	}
	// 与 MemStore 语义一致：不存在返回 ErrNotFound
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQLStore) GetProfile(uid int64) (*Profile, error) {
	p := &Profile{UID: uid}
	err := s.db.QueryRow(`SELECT u.nickname, p.elo_score, p.max_elo, p.wins, p.losses
 FROM user_profile p JOIN user u ON u.uid=p.uid WHERE p.uid=?`, uid).
		Scan(&p.Nickname, &p.Elo, &p.MaxElo, &p.Wins, &p.Losses)
	if err == sql.ErrNoRows {
		if _, err := s.db.Exec("INSERT INTO user_profile(uid) VALUES(?)", uid); err != nil && !isDup(err) {
			return nil, err
		}
		nick := ""
		if u, e := s.GetUser(uid); e == nil {
			nick = u.Nickname
		}
		return &Profile{UID: uid, Nickname: nick, Elo: 1000, MaxElo: 1000}, nil
	}
	return p, err
}

func (s *MySQLStore) UpdateElo(uid int64, newElo int, result int) error {
	if _, err := s.GetProfile(uid); err != nil { // 确保档案存在
		return err
	}
	winInc, loseInc := 0, 0
	if result > 0 {
		winInc = 1
	} else if result < 0 {
		loseInc = 1
	} // result==0 平局，胜负均不加
	_, err := s.db.Exec(
		"UPDATE user_profile SET elo_score=?, max_elo=GREATEST(max_elo,?), wins=wins+?, losses=losses+? WHERE uid=?",
		newElo, newElo, winInc, loseInc, uid)
	return err
}

func (s *MySQLStore) AddFriend(uid, friendUID int64) error {
	_, err := s.db.Exec("INSERT INTO user_friend(uid,friend_uid) VALUES(?,?)", uid, friendUID)
	if isDup(err) {
		return ErrDuplicate
	}
	return err
}

func (s *MySQLStore) DelFriend(uid, friendUID int64) error {
	_, err := s.db.Exec("DELETE FROM user_friend WHERE uid=? AND friend_uid=?", uid, friendUID)
	return err
}

func (s *MySQLStore) ListFriends(uid int64) ([]int64, error) {
	rows, err := s.db.Query("SELECT friend_uid FROM user_friend WHERE uid=?", uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *MySQLStore) InsertMatchRecord(r *MatchRecord) error {
	_, err := s.db.Exec(`INSERT INTO game_match_history
(uid,result,elo_change,final_coverage,painted_cells,straight_shots,lob_shots,
 hits_on_enemy,blackhole_destroyed,reflect_cnt,match_duration,start_time)
VALUES(?,?,?,?,?,?,?,?,?,?,?,NOW())`,
		r.UID, r.Result, r.EloChange, r.FinalCoverage, r.PaintedCells, r.StraightShots,
		r.LobShots, r.HitsOnEnemy, r.BlackholeDestroyed, r.ReflectCnt, r.MatchDuration)
	return err
}
