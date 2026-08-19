package store

import "sync"

type MemStore struct {
	mu       sync.Mutex
	nextUID  int64
	byName   map[string]*User
	byUID    map[int64]*User
	profiles map[int64]*Profile
	friends  map[int64]map[int64]bool
	records  []*MatchRecord
}

func NewMemStore() *MemStore {
	return &MemStore{
		byName:   make(map[string]*User),
		byUID:    make(map[int64]*User),
		profiles: make(map[int64]*Profile),
		friends:  make(map[int64]map[int64]bool),
	}
}

func (m *MemStore) CreateUser(username, hash string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byName[username]; ok {
		return 0, ErrDuplicate
	}
	m.nextUID++
	u := &User{UID: m.nextUID, Username: username, PasswordHash: hash} // Nickname 初始为空
	m.byName[username] = u
	m.byUID[m.nextUID] = u
	return m.nextUID, nil
}

func (m *MemStore) GetUser(uid int64) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byUID[uid]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *MemStore) SetNickname(uid int64, nickname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byUID[uid]
	if !ok {
		return ErrNotFound
	}
	u.Nickname = nickname
	if p, ok := m.profiles[uid]; ok {
		p.Nickname = nickname
	}
	return nil
}

func (m *MemStore) GetUserByName(username string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byName[username]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *MemStore) GetProfile(uid int64) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[uid]
	if !ok {
		p = &Profile{UID: uid, Elo: 1000, MaxElo: 1000}
		m.profiles[uid] = p
	}
	if u, ok := m.byUID[uid]; ok {
		p.Nickname = u.Nickname // 从 user 表同步显示名
	}
	cp := *p
	return &cp, nil
}

func (m *MemStore) UpdateElo(uid int64, newElo int, result int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[uid]
	if !ok {
		p = &Profile{UID: uid, Elo: 1000, MaxElo: 1000}
		m.profiles[uid] = p
	}
	p.Elo = newElo
	if newElo > p.MaxElo {
		p.MaxElo = newElo
	}
	if result > 0 {
		p.Wins++
	} else if result < 0 {
		p.Losses++
	} // result==0 平局，胜负均不加
	return nil
}

func (m *MemStore) AddFriend(uid, friendUID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.friends[uid] == nil {
		m.friends[uid] = make(map[int64]bool)
	}
	if m.friends[uid][friendUID] {
		return ErrDuplicate
	}
	m.friends[uid][friendUID] = true
	return nil
}

func (m *MemStore) DelFriend(uid, friendUID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.friends[uid], friendUID)
	return nil
}

func (m *MemStore) ListFriends(uid int64) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []int64
	for id := range m.friends[uid] {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *MemStore) InsertMatchRecord(r *MatchRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.records = append(m.records, &cp)
	return nil
}
