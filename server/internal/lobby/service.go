// service.go — 大厅业务层：注册/登录/好友/档案/昵称/匹配 handlers、authed 鉴权中间件、撮合循环与断连清理
package lobby

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/gateway"
	"signaldrift/server/internal/protocol"
	"signaldrift/server/internal/store"
)

type Service struct {
	cfg    *config.LobbyConfig
	st     store.Store
	pool   *MatchPool
	pres   *Presence
	eq     *EventQueue
	tokens *TokenIssuer
	// 房间分配：计划 4 注入真实分配，本计划默认自增 stub
	allocRoom  func(MatchPair) (int64, error)
	stubRoomID atomic.Int64
}

func NewService(cfg *config.LobbyConfig, st store.Store, pool *MatchPool,
	pres *Presence, eq *EventQueue, tokens *TokenIssuer) *Service {
	svc := &Service{cfg: cfg, st: st, pool: pool, pres: pres, eq: eq, tokens: tokens}
	svc.allocRoom = func(MatchPair) (int64, error) { return svc.stubRoomID.Add(1), nil }
	return svc
}

// SetRoomAllocator 注入房间分配器：仅可在 RunMatchLoop 启动前调用（非并发安全）
func (svc *Service) SetRoomAllocator(fn func(MatchPair) (int64, error)) { svc.allocRoom = fn }

func reply(s *gateway.Session, msgID uint16, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		log.Printf("ERROR marshal resp: %v", err)
		return
	}
	s.Send(msgID, body)
}

func replyErr(s *gateway.Session, code int, msg string) {
	reply(s, protocol.MsgErrorResp, ErrorResp{Code: code, Msg: msg})
}

func (svc *Service) Mount(r *gateway.Router) {
	r.Register(protocol.MsgRegisterReq, svc.handleRegister)
	r.Register(protocol.MsgLoginReq, svc.handleLogin)
	r.Register(protocol.MsgFriendAdd, svc.authed(svc.handleFriendAdd))
	r.Register(protocol.MsgFriendDel, svc.authed(svc.handleFriendDel))
	r.Register(protocol.MsgFriendList, svc.authed(svc.handleFriendList))
	r.Register(protocol.MsgProfileReq, svc.authed(svc.handleProfile))
	r.Register(protocol.MsgSetNickname, svc.authed(svc.handleSetNickname))
	r.Register(protocol.MsgMatchReq, svc.authed(svc.handleMatchReq))
	r.Register(protocol.MsgMatchCancel, svc.authed(svc.handleMatchCancel))
}

func (svc *Service) authed(h gateway.HandlerFunc) gateway.HandlerFunc {
	return func(s *gateway.Session, f *protocol.Frame) {
		if s.UID == 0 {
			replyErr(s, 401, "not logged in")
			return
		}
		h(s, f)
	}
}

func (svc *Service) handleRegister(s *gateway.Session, f *protocol.Frame) {
	var req RegisterReq
	if json.Unmarshal(f.Body, &req) != nil {
		reply(s, protocol.MsgRegisterResp, RegisterResp{Code: 400})
		return
	}
	// 用户名：下限按 rune（≥3 字符，中英文一致）；上限按字节（≤32，中文约 10 字）
	n := len([]rune(req.Username))
	if n < 3 || len(req.Username) > 32 ||
		len(req.Password) < 6 || len(req.Password) > 64 {
		reply(s, protocol.MsgRegisterResp, RegisterResp{Code: 400})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		reply(s, protocol.MsgRegisterResp, RegisterResp{Code: 500})
		return
	}
	uid, err := svc.st.CreateUser(req.Username, string(hash))
	if err == store.ErrDuplicate {
		reply(s, protocol.MsgRegisterResp, RegisterResp{Code: 409})
		return
	}
	if err != nil {
		reply(s, protocol.MsgRegisterResp, RegisterResp{Code: 500})
		return
	}
	reply(s, protocol.MsgRegisterResp, RegisterResp{Code: 0, UID: uid})
}

func (svc *Service) handleLogin(s *gateway.Session, f *protocol.Frame) {
	var req LoginReq
	if json.Unmarshal(f.Body, &req) != nil {
		reply(s, protocol.MsgLoginResp, LoginResp{Code: 400})
		return
	}
	u, err := svc.st.GetUserByName(req.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		reply(s, protocol.MsgLoginResp, LoginResp{Code: 403})
		return
	}
	// 重复登录：踢旧会话
	if old, ok := svc.pres.Get(u.UID); ok && old != s {
		old.Close()
	}
	s.UID = u.UID
	svc.pres.Bind(u.UID, s)
	p, err := svc.st.GetProfile(u.UID)
	if err != nil {
		reply(s, protocol.MsgLoginResp, LoginResp{Code: 500})
		return
	}
	reply(s, protocol.MsgLoginResp, LoginResp{
		Code: 0, UID: u.UID, Nickname: p.Nickname, Elo: p.Elo,
		Token: svc.tokens.Issue(u.UID), ExpSec: svc.tokens.Expiry(),
	})
}

func (svc *Service) handleFriendAdd(s *gateway.Session, f *protocol.Frame) {
	var req FriendAddReq
	if json.Unmarshal(f.Body, &req) != nil || req.FriendUID == s.UID {
		reply(s, protocol.MsgFriendAddOK, ErrorResp{Code: 400})
		return
	}
	// 校验好友存在：不存在回 404，防幽灵好友（schema 无外键，Store 层不拦）
	if _, err := svc.st.GetUser(req.FriendUID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			reply(s, protocol.MsgFriendAddOK, ErrorResp{Code: 404})
		} else {
			reply(s, protocol.MsgFriendAddOK, ErrorResp{Code: 500})
		}
		return
	}
	switch err := svc.st.AddFriend(s.UID, req.FriendUID); err {
	case nil:
		reply(s, protocol.MsgFriendAddOK, ErrorResp{Code: 0})
	case store.ErrDuplicate:
		reply(s, protocol.MsgFriendAddOK, ErrorResp{Code: 409})
	default:
		reply(s, protocol.MsgFriendAddOK, ErrorResp{Code: 500})
	}
}

func (svc *Service) handleFriendDel(s *gateway.Session, f *protocol.Frame) {
	var req FriendDelReq
	if json.Unmarshal(f.Body, &req) != nil {
		reply(s, protocol.MsgFriendDelOK, ErrorResp{Code: 400})
		return
	}
	if err := svc.st.DelFriend(s.UID, req.FriendUID); err != nil {
		reply(s, protocol.MsgFriendDelOK, ErrorResp{Code: 500})
		return
	}
	reply(s, protocol.MsgFriendDelOK, ErrorResp{Code: 0})
}

func (svc *Service) handleFriendList(s *gateway.Session, f *protocol.Frame) {
	ids, err := svc.st.ListFriends(s.UID)
	if err != nil {
		reply(s, protocol.MsgFriendListOK, FriendListResp{Code: 500})
		return
	}
	resp := FriendListResp{Code: 0}
	for _, id := range ids {
		p, err := svc.st.GetProfile(id)
		if err != nil {
			continue
		}
		resp.Friends = append(resp.Friends, FriendInfo{
			UID: id, Nickname: p.Nickname, Elo: p.Elo, Online: svc.pres.IsOnline(id),
		})
	}
	reply(s, protocol.MsgFriendListOK, resp)
}

func (svc *Service) handleProfile(s *gateway.Session, f *protocol.Frame) {
	p, err := svc.st.GetProfile(s.UID)
	if err != nil {
		reply(s, protocol.MsgProfileResp, ProfileResp{Code: 500})
		return
	}
	reply(s, protocol.MsgProfileResp, ProfileResp{
		Code: 0, UID: p.UID, Nickname: p.Nickname, Elo: p.Elo, MaxElo: p.MaxElo, Wins: p.Wins, Losses: p.Losses,
	})
}

// handleSetNickname 玩家注册后自行设置/修改游戏内显示名（1-16 字符，允许中文）
func (svc *Service) handleSetNickname(s *gateway.Session, f *protocol.Frame) {
	var req SetNicknameReq
	nick := ""
	if json.Unmarshal(f.Body, &req) == nil {
		nick = strings.TrimSpace(req.Nickname)
	}
	// 按 rune 计长度，支持中文
	if n := len([]rune(nick)); n < 1 || n > 16 {
		reply(s, protocol.MsgSetNicknameOK, SetNicknameResp{Code: 400})
		return
	}
	if err := svc.st.SetNickname(s.UID, nick); err != nil {
		reply(s, protocol.MsgSetNicknameOK, SetNicknameResp{Code: 500})
		return
	}
	reply(s, protocol.MsgSetNicknameOK, SetNicknameResp{Code: 0, Nickname: nick})
}

// OnSessionClosed 网关断连回调：清理在线状态与匹配池
func (svc *Service) OnSessionClosed(s *gateway.Session) {
	if s.UID != 0 {
		if cur, ok := svc.pres.Get(s.UID); ok && cur == s {
			svc.pres.Unbind(s.UID)
			svc.pool.Cancel(s.UID) // 仅当仍是当前会话时才取消匹配（旧会话回调不误伤新会话）
		}
	}
}

func (svc *Service) handleMatchReq(s *gateway.Session, f *protocol.Frame) {
	p, err := svc.st.GetProfile(s.UID)
	if err != nil {
		reply(s, protocol.MsgMatchResp, ErrorResp{Code: 500})
		return
	}
	if !svc.pool.Add(s.UID, p.Elo) {
		reply(s, protocol.MsgMatchResp, ErrorResp{Code: 409, Msg: "already in pool"})
		return
	}
	reply(s, protocol.MsgMatchResp, ErrorResp{Code: 0})
}

func (svc *Service) handleMatchCancel(s *gateway.Session, f *protocol.Frame) {
	if svc.pool.Cancel(s.UID) {
		reply(s, protocol.MsgMatchCancelOK, ErrorResp{Code: 0})
	} else {
		reply(s, protocol.MsgMatchCancelOK, ErrorResp{Code: 404})
	}
}

func (svc *Service) pollMatchesOnce() {
	for _, pair := range svc.pool.Poll() {
		roomID, err := svc.allocRoom(pair)
		if err != nil {
			log.Printf("ERROR alloc room: %v", err)
			// 分配失败：在线者放回池（离线玩家不回池，避免被反复配对空耗房间）
			if svc.pres.IsOnline(pair.UIDA) {
				svc.pool.Add(pair.UIDA, pair.EloA)
			}
			if svc.pres.IsOnline(pair.UIDB) {
				svc.pool.Add(pair.UIDB, pair.EloB)
			}
			continue
		}
		// 已知语义：Poll 与推送之间断线的玩家，房间已分配、推送静默丢失，
		// 重连后需重新入池（计划 4 接真实分配器时考虑在线性前移/释放房间）
		nickA, nickB := "", ""
		if u, e := svc.st.GetUser(pair.UIDA); e == nil {
			nickA = u.Nickname
		}
		if u, e := svc.st.GetUser(pair.UIDB); e == nil {
			nickB = u.Nickname
		}
		if sa, ok := svc.pres.Get(pair.UIDA); ok {
			reply(sa, protocol.MsgMatchFound, MatchFoundPush{RoomID: roomID, OpponentUID: pair.UIDB, OppNickname: nickB, OppElo: pair.EloB})
		}
		if sb, ok := svc.pres.Get(pair.UIDB); ok {
			reply(sb, protocol.MsgMatchFound, MatchFoundPush{RoomID: roomID, OpponentUID: pair.UIDA, OppNickname: nickA, OppElo: pair.EloA})
		}
		log.Printf("INFO match found room=%d a=%d b=%d", roomID, pair.UIDA, pair.UIDB)
	}
}

func (svc *Service) RunMatchLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second // 防 time.NewTicker panic
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			svc.pollMatchesOnce()
		}
	}
}
