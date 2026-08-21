// service_test.go — 大厅业务测试：注册登录/好友/档案/昵称/中文用户名边界/踢旧会话（走真实 Router 分发）
package lobby

import (
	"bytes"
	"encoding/json"
	"net"
	"testing"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/gateway"
	"signaldrift/server/internal/protocol"
	"signaldrift/server/internal/store"
)

func testService(t *testing.T) (*Service, *gateway.Router, *gateway.SessionManager) {
	t.Helper()
	cfg := &config.LobbyConfig{
		TokenSecret: "s", TokenTTLSec: 3600,
		Match: config.MatchConfig{BaseGap: 100, GapStep: 100, WidenSec: 30},
		EloK:  32, QueueSize: 16,
	}
	st := store.NewMemStore()
	eq := NewEventQueue(st, 16)
	eq.Start()
	t.Cleanup(eq.Stop)
	svc := NewService(cfg, st, NewMatchPool(cfg.Match, nil), NewPresence(), eq,
		NewTokenIssuer(cfg.TokenSecret, cfg.TokenTTLSec, nil))
	r := gateway.NewRouter()
	svc.Mount(r)
	return svc, r, gateway.NewSessionManager(16)
}

func newSess(t *testing.T, m *gateway.SessionManager) *gateway.Session {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	return m.Create(c1)
}

func send(r *gateway.Router, s *gateway.Session, msgID uint16, v any) {
	body, _ := json.Marshal(v)
	r.Dispatch(s, &protocol.Frame{MsgID: msgID, Body: body})
}

func recv[T any](t *testing.T, s *gateway.Session, wantID uint16) T {
	t.Helper()
	raw := <-s.SendQueue()
	f, err := protocol.NewFrameReader(bytes.NewReader(raw)).Next()
	if err != nil || f.MsgID != wantID {
		t.Fatalf("msgID=%d want=%d err=%v", f.MsgID, wantID, err)
	}
	var out T
	if err := json.Unmarshal(f.Body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// regLogin 注册+登录一个用户并返回 uid（名字须 ≥3 字符）
func regLogin(t *testing.T, r *gateway.Router, s *gateway.Session, name string) int64 {
	t.Helper()
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: name, Password: "123456"})
	reg := recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	if reg.Code != 0 || reg.UID == 0 {
		t.Fatalf("register failed: %+v", reg)
	}
	send(r, s, protocol.MsgLoginReq, LoginReq{Username: name, Password: "123456"})
	lg := recv[LoginResp](t, s, protocol.MsgLoginResp)
	if lg.Code != 0 {
		t.Fatalf("login failed: %+v", lg)
	}
	return reg.UID
}

func TestRegisterDuplicate409(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "dup1", Password: "123456"})
	reg := recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	if reg.Code != 0 {
		t.Fatalf("first register failed: %+v", reg)
	}
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "dup1", Password: "123456"})
	reg = recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	if reg.Code != 409 {
		t.Fatalf("want 409 got %d", reg.Code)
	}
}

func TestFriendAddDuplicate409(t *testing.T) {
	_, r, m := testService(t)
	s1, s2 := newSess(t, m), newSess(t, m)
	regLogin(t, r, s1, "fdup1")
	u2 := regLogin(t, r, s2, "fdup2")
	send(r, s1, protocol.MsgFriendAdd, FriendAddReq{FriendUID: u2})
	e := recv[ErrorResp](t, s1, protocol.MsgFriendAddOK)
	if e.Code != 0 {
		t.Fatalf("first add failed: %d", e.Code)
	}
	send(r, s1, protocol.MsgFriendAdd, FriendAddReq{FriendUID: u2})
	e = recv[ErrorResp](t, s1, protocol.MsgFriendAddOK)
	if e.Code != 409 {
		t.Fatalf("want 409 got %d", e.Code)
	}
}

func TestFriendDel(t *testing.T) {
	_, r, m := testService(t)
	s1, s2 := newSess(t, m), newSess(t, m)
	regLogin(t, r, s1, "fdel1")
	u2 := regLogin(t, r, s2, "fdel2")
	send(r, s1, protocol.MsgFriendAdd, FriendAddReq{FriendUID: u2})
	recv[ErrorResp](t, s1, protocol.MsgFriendAddOK)
	send(r, s1, protocol.MsgFriendDel, FriendDelReq{FriendUID: u2})
	e := recv[ErrorResp](t, s1, protocol.MsgFriendDelOK)
	if e.Code != 0 {
		t.Fatalf("del failed: %d", e.Code)
	}
	send(r, s1, protocol.MsgFriendList, struct{}{})
	fl := recv[FriendListResp](t, s1, protocol.MsgFriendListOK)
	if len(fl.Friends) != 0 {
		t.Fatalf("friends must be empty after del: %+v", fl)
	}
}

func TestProfile(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	regLogin(t, r, s, "pro1")
	send(r, s, protocol.MsgProfileReq, struct{}{})
	p := recv[ProfileResp](t, s, protocol.MsgProfileResp)
	if p.Code != 0 || p.UID == 0 || p.Elo != 1000 || p.MaxElo != 1000 || p.Wins != 0 || p.Losses != 0 {
		t.Fatalf("p=%+v", p)
	}
}

func TestSetNickname(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	regLogin(t, r, s, "nick1")
	// 设置中文昵称（1-16 rune）
	send(r, s, protocol.MsgSetNickname, SetNicknameReq{Nickname: "信号漂流"})
	np := recv[SetNicknameResp](t, s, protocol.MsgSetNicknameOK)
	if np.Code != 0 || np.Nickname != "信号漂流" {
		t.Fatalf("np=%+v", np)
	}
	// 17 个中文字 → 400
	send(r, s, protocol.MsgSetNickname, SetNicknameReq{Nickname: "信号漂流信号漂流信号漂流信号漂流漂"})
	np = recv[SetNicknameResp](t, s, protocol.MsgSetNicknameOK)
	if np.Code != 400 {
		t.Fatalf("17-char nickname must fail, got %d", np.Code)
	}
	// 空白昵称（TrimSpace 后为空）→ 400
	send(r, s, protocol.MsgSetNickname, SetNicknameReq{Nickname: "   "})
	np = recv[SetNicknameResp](t, s, protocol.MsgSetNicknameOK)
	if np.Code != 400 {
		t.Fatalf("blank nickname must fail, got %d", np.Code)
	}
	// 修改昵称 + 档案同步
	send(r, s, protocol.MsgSetNickname, SetNicknameReq{Nickname: "新昵称"})
	np = recv[SetNicknameResp](t, s, protocol.MsgSetNicknameOK)
	if np.Code != 0 || np.Nickname != "新昵称" {
		t.Fatalf("np=%+v", np)
	}
	send(r, s, protocol.MsgProfileReq, struct{}{})
	p := recv[ProfileResp](t, s, protocol.MsgProfileResp)
	if p.Nickname != "新昵称" {
		t.Fatalf("profile nickname not synced: %+v", p)
	}
}

func TestRegisterLoginFlow(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)

	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "alice", Password: "123456"})
	reg := recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	if reg.Code != 0 || reg.UID == 0 {
		t.Fatalf("reg=%+v", reg)
	}

	send(r, s, protocol.MsgLoginReq, LoginReq{Username: "alice", Password: "123456"})
	lg := recv[LoginResp](t, s, protocol.MsgLoginResp)
	if lg.Code != 0 || lg.UID != reg.UID || lg.Elo != 1000 || lg.Token == "" {
		t.Fatalf("lg=%+v", lg)
	}
	if lg.ExpSec == 0 {
		t.Fatalf("login exp must be set: %+v", lg)
	}
	if s.UID != reg.UID {
		t.Fatal("session UID not bound")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "bob", Password: "123456"})
	recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	send(r, s, protocol.MsgLoginReq, LoginReq{Username: "bob", Password: "wrong!"})
	lg := recv[LoginResp](t, s, protocol.MsgLoginResp)
	if lg.Code == 0 {
		t.Fatal("wrong password must fail")
	}
}

func TestFriendRequiresLogin(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	send(r, s, protocol.MsgFriendAdd, FriendAddReq{FriendUID: 9})
	e := recv[ErrorResp](t, s, protocol.MsgErrorResp)
	if e.Code != 401 {
		t.Fatalf("want 401 got %d", e.Code)
	}
}

func TestRegisterChineseUsername(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	// 下限按 rune：2 个中文字（6 字节）不足 3 字符 → 400
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "测试", Password: "123456"})
	reg := recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	if reg.Code != 400 {
		t.Fatalf("2-char chinese username must fail, got %d", reg.Code)
	}
	// 3 个中文字：rune=3 达标，9 字节 ≤32 → 成功
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "测试名", Password: "123456"})
	reg = recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	if reg.Code != 0 {
		t.Fatalf("3-char chinese username must pass, got %d", reg.Code)
	}
	// 11 个中文字 = 33 字节 > 32 → 400（中文上限 10 字）
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "测测测测测测测测测测测", Password: "123456"})
	reg = recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	if reg.Code != 400 {
		t.Fatalf("11-char chinese username must fail, got %d", reg.Code)
	}
}

func TestLoginKicksOldSession(t *testing.T) {
	_, r, m := testService(t)
	s1, s2 := newSess(t, m), newSess(t, m)
	send(r, s1, protocol.MsgRegisterReq, RegisterReq{Username: "kick", Password: "123456"})
	recv[RegisterResp](t, s1, protocol.MsgRegisterResp)
	send(r, s1, protocol.MsgLoginReq, LoginReq{Username: "kick", Password: "123456"})
	recv[LoginResp](t, s1, protocol.MsgLoginResp)
	select {
	case <-s1.Done():
		t.Fatal("s1 must still be alive before second login")
	default:
	}
	// 同 UID 二次登录：旧会话 s1 被踢
	send(r, s2, protocol.MsgLoginReq, LoginReq{Username: "kick", Password: "123456"})
	recv[LoginResp](t, s2, protocol.MsgLoginResp)
	select {
	case <-s1.Done():
	default:
		t.Fatal("old session must be closed")
	}
	if s2.UID == 0 {
		t.Fatal("s2 must be bound")
	}
}

func TestFriendAddNotFound(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "n1x", Password: "123456"})
	recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	send(r, s, protocol.MsgLoginReq, LoginReq{Username: "n1x", Password: "123456"})
	recv[LoginResp](t, s, protocol.MsgLoginResp)
	send(r, s, protocol.MsgFriendAdd, FriendAddReq{FriendUID: 99999})
	e := recv[ErrorResp](t, s, protocol.MsgFriendAddOK)
	if e.Code != 404 {
		t.Fatalf("want 404 got %d", e.Code)
	}
}

func TestFriendAddSelf(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: "self", Password: "123456"})
	reg := recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	send(r, s, protocol.MsgLoginReq, LoginReq{Username: "self", Password: "123456"})
	recv[LoginResp](t, s, protocol.MsgLoginResp)
	send(r, s, protocol.MsgFriendAdd, FriendAddReq{FriendUID: reg.UID})
	e := recv[ErrorResp](t, s, protocol.MsgFriendAddOK)
	if e.Code != 400 {
		t.Fatalf("want 400 got %d", e.Code)
	}
}

func TestFriendAddListFlow(t *testing.T) {
	svc, r, m := testService(t)
	s1, s2 := newSess(t, m), newSess(t, m)
	// 两个账号
	send(r, s1, protocol.MsgRegisterReq, RegisterReq{Username: "u1x", Password: "123456"})
	u1 := recv[RegisterResp](t, s1, protocol.MsgRegisterResp)
	send(r, s2, protocol.MsgRegisterReq, RegisterReq{Username: "u2x", Password: "123456"})
	u2 := recv[RegisterResp](t, s2, protocol.MsgRegisterResp)
	send(r, s1, protocol.MsgLoginReq, LoginReq{Username: "u1x", Password: "123456"})
	recv[LoginResp](t, s1, protocol.MsgLoginResp)
	send(r, s2, protocol.MsgLoginReq, LoginReq{Username: "u2x", Password: "123456"})
	recv[LoginResp](t, s2, protocol.MsgLoginResp)

	send(r, s1, protocol.MsgFriendAdd, FriendAddReq{FriendUID: u2.UID})
	recv[ErrorResp](t, s1, protocol.MsgFriendAddOK)
	send(r, s1, protocol.MsgFriendList, struct{}{})
	fl := recv[FriendListResp](t, s1, protocol.MsgFriendListOK)
	if len(fl.Friends) != 1 || fl.Friends[0].UID != u2.UID || !fl.Friends[0].Online {
		t.Fatalf("fl=%+v", fl)
	}
	// u2 断开后在线状态变化
	svc.OnSessionClosed(s2)
	send(r, s1, protocol.MsgFriendList, struct{}{})
	fl = recv[FriendListResp](t, s1, protocol.MsgFriendListOK)
	if fl.Friends[0].Online {
		t.Fatal("u2 must be offline")
	}
	_ = u1
}
