package store

import "testing"

func TestMemUserLifecycle(t *testing.T) {
	s := NewMemStore()
	uid, err := s.CreateUser("alice", "hash1")
	if err != nil || uid == 0 {
		t.Fatalf("uid=%d err=%v", uid, err)
	}
	if _, err := s.CreateUser("alice", "hash2"); err != ErrDuplicate {
		t.Fatalf("want ErrDuplicate got %v", err)
	}
	u, err := s.GetUserByName("alice")
	if err != nil || u.UID != uid || u.PasswordHash != "hash1" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
	if _, err := s.GetUserByName("nobody"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestMemProfileEloFlow(t *testing.T) {
	s := NewMemStore()
	uid, _ := s.CreateUser("bob", "h")
	p, err := s.GetProfile(uid)
	if err != nil || p.Elo != 1000 || p.MaxElo != 1000 {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	s.UpdateElo(uid, 1016, 1)
	p, _ = s.GetProfile(uid)
	if p.Elo != 1016 || p.MaxElo != 1016 || p.Wins != 1 {
		t.Fatalf("p=%+v", p)
	}
	s.UpdateElo(uid, 1000, -1)
	p, _ = s.GetProfile(uid)
	if p.Elo != 1000 || p.MaxElo != 1016 || p.Losses != 1 {
		t.Fatalf("p=%+v", p)
	}
}

func TestMemFriends(t *testing.T) {
	s := NewMemStore()
	a, _ := s.CreateUser("a", "h")
	b, _ := s.CreateUser("b", "h")
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
	s.DelFriend(a, b)
	ids, _ = s.ListFriends(a)
	if len(ids) != 0 {
		t.Fatalf("ids=%v", ids)
	}
}
