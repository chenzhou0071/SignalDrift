// match_service_test.go — 匹配业务测试：撮合推送/取消/重复入池 409/取消 404/旧会话回调不误伤匹配项
package lobby

import (
	"errors"
	"testing"

	"signaldrift/server/internal/gateway"
	"signaldrift/server/internal/protocol"
)

func TestMatchFlowPush(t *testing.T) {
	svc, r, m := testService(t)
	s1, s2 := newSess(t, m), newSess(t, m)
	registerAndLogin(t, r, s1, "p1")
	registerAndLogin(t, r, s2, "p2")

	send(r, s1, protocol.MsgMatchReq, struct{}{})
	recv[ErrorResp](t, s1, protocol.MsgMatchResp)
	send(r, s2, protocol.MsgMatchReq, struct{}{})
	recv[ErrorResp](t, s2, protocol.MsgMatchResp)

	svc.pollMatchesOnce() // 导出给测试的单次撮合
	f1 := recv[MatchFoundPush](t, s1, protocol.MsgMatchFound)
	f2 := recv[MatchFoundPush](t, s2, protocol.MsgMatchFound)
	if f1.RoomID == 0 || f1.RoomID != f2.RoomID {
		t.Fatalf("f1=%+v f2=%+v", f1, f2)
	}
	if f1.OpponentUID != s2.UID || f2.OpponentUID != s1.UID {
		t.Fatalf("opp mismatch")
	}
	if f1.OppElo != 1000 || f2.OppElo != 1000 {
		t.Fatalf("elo mismatch: f1=%+v f2=%+v", f1, f2)
	}
}

func TestMatchDuplicateReq409(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	registerAndLogin(t, r, s, "dup")
	send(r, s, protocol.MsgMatchReq, struct{}{})
	recv[ErrorResp](t, s, protocol.MsgMatchResp)
	send(r, s, protocol.MsgMatchReq, struct{}{})
	e := recv[ErrorResp](t, s, protocol.MsgMatchResp)
	if e.Code != 409 {
		t.Fatalf("want 409 got %d", e.Code)
	}
}

func TestMatchCancelNotFound404(t *testing.T) {
	_, r, m := testService(t)
	s := newSess(t, m)
	registerAndLogin(t, r, s, "cnf")
	send(r, s, protocol.MsgMatchCancel, struct{}{})
	e := recv[ErrorResp](t, s, protocol.MsgMatchCancelOK)
	if e.Code != 404 {
		t.Fatalf("want 404 got %d", e.Code)
	}
}

// 容错分支：房间分配失败 → 双方放回匹配池，且不推送 MatchFound
func TestMatchAllocFailRefillPool(t *testing.T) {
	svc, r, m := testService(t)
	svc.SetRoomAllocator(func(MatchPair) (int64, error) {
		return 0, errors.New("room alloc failed")
	})
	s1, s2 := newSess(t, m), newSess(t, m)
	registerAndLogin(t, r, s1, "af1")
	registerAndLogin(t, r, s2, "af2")
	send(r, s1, protocol.MsgMatchReq, struct{}{})
	recv[ErrorResp](t, s1, protocol.MsgMatchResp)
	send(r, s2, protocol.MsgMatchReq, struct{}{})
	recv[ErrorResp](t, s2, protocol.MsgMatchResp)

	svc.pollMatchesOnce()

	if svc.pool.Size() != 2 {
		t.Fatalf("both players must be back in pool, size=%d", svc.pool.Size())
	}
	// 分配失败：双方都不应收到 MatchFound
	select {
	case raw := <-s1.SendQueue():
		t.Fatalf("unexpected push to s1: %x", raw)
	default:
	}
	select {
	case raw := <-s2.SendQueue():
		t.Fatalf("unexpected push to s2: %x", raw)
	default:
	}
}

func TestMatchCancel(t *testing.T) {
	svc, r, m := testService(t)
	s1 := newSess(t, m)
	registerAndLogin(t, r, s1, "p3")
	send(r, s1, protocol.MsgMatchReq, struct{}{})
	recv[ErrorResp](t, s1, protocol.MsgMatchResp)
	send(r, s1, protocol.MsgMatchCancel, struct{}{})
	recv[ErrorResp](t, s1, protocol.MsgMatchCancelOK)
	svc.pollMatchesOnce()
	if svc.pool.Size() != 0 {
		t.Fatal("pool must be empty")
	}
}

// helper：注册+登录（追加到本文件）
func registerAndLogin(t *testing.T, r *gateway.Router, s *gateway.Session, name string) {
	t.Helper()
	send(r, s, protocol.MsgRegisterReq, RegisterReq{Username: name + "xx", Password: "123456"})
	recv[RegisterResp](t, s, protocol.MsgRegisterResp)
	send(r, s, protocol.MsgLoginReq, LoginReq{Username: name + "xx", Password: "123456"})
	recv[LoginResp](t, s, protocol.MsgLoginResp)
}

// W1 修复测试：旧会话被踢后其断连回调延迟到达，不得取消新会话的匹配项
func TestOldSessionCallbackNoCancelMatch(t *testing.T) {
	svc, r, m := testService(t)
	s1, s2 := newSess(t, m), newSess(t, m)
	regLogin(t, r, s1, "kickm")
	// 同 UID 二次登录（不注册）：s1 被踢
	send(r, s2, protocol.MsgLoginReq, LoginReq{Username: "kickm", Password: "123456"})
	recv[LoginResp](t, s2, protocol.MsgLoginResp)
	send(r, s2, protocol.MsgMatchReq, struct{}{})
	recv[ErrorResp](t, s2, protocol.MsgMatchResp)
	// 旧会话断连回调延迟到达
	svc.OnSessionClosed(s1)
	if svc.pool.Size() != 1 {
		t.Fatalf("old session callback must not cancel new session match, size=%d", svc.pool.Size())
	}
}
