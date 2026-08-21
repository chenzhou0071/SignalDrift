// dto.go — 大厅消息体：注册/登录/匹配/好友/档案/昵称的请求应答 JSON 结构体
package lobby

type ErrorResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type RegisterReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type RegisterResp struct {
	Code int   `json:"code"`
	UID  int64 `json:"uid"`
}

type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type LoginResp struct {
	Code     int    `json:"code"`
	UID      int64  `json:"uid"`
	Nickname string `json:"nickname"` // 空字符串=尚未设置，客户端弹设名界面
	Elo      int    `json:"elo"`
	Token    string `json:"token"` // 重连 Token
	ExpMsec  int64  `json:"exp"`
}

// SetNicknameReq 玩家注册后自行设置/修改游戏内显示名（可中文）
type SetNicknameReq struct{ Nickname string `json:"nickname"` }
type SetNicknameResp struct {
	Code     int    `json:"code"`
	Nickname string `json:"nickname"`
}

type MatchFoundPush struct {
	RoomID      int64  `json:"room_id"`
	OpponentUID int64  `json:"opp_uid"`
	OppNickname string `json:"opp_nickname"`
	OppElo      int    `json:"opp_elo"`
}

type FriendAddReq struct{ FriendUID int64 `json:"friend_uid"` }
type FriendDelReq struct{ FriendUID int64 `json:"friend_uid"` }
type FriendInfo struct {
	UID      int64  `json:"uid"`
	Nickname string `json:"nickname"`
	Elo      int    `json:"elo"`
	Online   bool   `json:"online"`
}
type FriendListResp struct {
	Code    int          `json:"code"`
	Friends []FriendInfo `json:"friends"`
}

type ProfileResp struct {
	Code     int    `json:"code"`
	UID      int64  `json:"uid"`
	Nickname string `json:"nickname"`
	Elo      int    `json:"elo"`
	MaxElo   int    `json:"max_elo"`
	Wins     int    `json:"wins"`
	Losses   int    `json:"losses"`
}
