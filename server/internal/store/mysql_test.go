//go:build integration

package store

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
)

func mustMySQL(t *testing.T) *MySQLStore {
	dsn := os.Getenv("SD_TEST_DSN")
	if dsn == "" {
		t.Skip("SD_TEST_DSN not set")
	}
	s, err := NewMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func randName() string { return fmt.Sprintf("u%d", rand.Int63()) }

func TestMySQLUserLifecycle(t *testing.T) {
	s := mustMySQL(t)
	name := randName()
	uid, err := s.CreateUser(name, "hash1")
	if err != nil || uid == 0 {
		t.Fatalf("uid=%d err=%v", uid, err)
	}
	if _, err := s.CreateUser(name, "hash2"); err != ErrDuplicate {
		t.Fatalf("want dup got %v", err)
	}
	u, err := s.GetUserByName(name)
	if err != nil || u.UID != uid {
		t.Fatalf("u=%+v err=%v", u, err)
	}
}

func TestMySQLProfileAndFriend(t *testing.T) {
	s := mustMySQL(t)
	a, _ := s.CreateUser(randName(), "h")
	b, _ := s.CreateUser(randName(), "h")
	p, err := s.GetProfile(a)
	if err != nil || p.Elo != 1000 {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	if err := s.UpdateElo(a, 1016, 1); err != nil {
		t.Fatal(err)
	}
	p, _ = s.GetProfile(a)
	if p.Elo != 1016 || p.Wins != 1 {
		t.Fatalf("p=%+v", p)
	}
	if err := s.AddFriend(a, b); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFriend(a, b); err != ErrDuplicate {
		t.Fatalf("want dup got %v", err)
	}
	ids, _ := s.ListFriends(a)
	if len(ids) != 1 || ids[0] != b {
		t.Fatalf("ids=%v", ids)
	}
}

func TestMySQLSetNickname(t *testing.T) {
	s := mustMySQL(t)
	uid, _ := s.CreateUser(randName(), "h")
	if err := s.SetNickname(uid, "小明"); err != nil { // utf8mb4 中文
		t.Fatal(err)
	}
	u, err := s.GetUser(uid)
	if err != nil || u.Nickname != "小明" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
	if err := s.SetNickname(99999999, "x"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}
