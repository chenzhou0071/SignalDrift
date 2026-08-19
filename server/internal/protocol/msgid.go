package protocol

// 消息 ID 分配：1-99 网关层，100-199 测试/通用，200-299 大厅（计划2），300-399 房间（计划4）
const (
	MsgHeartbeat    uint16 = 1
	MsgHeartbeatAck uint16 = 2
	MsgEcho         uint16 = 100
)

// 大厅 200-299
const (
	MsgRegisterReq   uint16 = 200
	MsgRegisterResp  uint16 = 201
	MsgLoginReq      uint16 = 202
	MsgLoginResp     uint16 = 203
	MsgMatchReq      uint16 = 210
	MsgMatchResp     uint16 = 211 // 入池确认
	MsgMatchCancel   uint16 = 212
	MsgMatchCancelOK uint16 = 213
	MsgMatchFound    uint16 = 215 // 服务端推送：匹配成功
	MsgFriendAdd     uint16 = 220
	MsgFriendAddOK   uint16 = 221
	MsgFriendDel     uint16 = 222
	MsgFriendDelOK   uint16 = 223
	MsgFriendList    uint16 = 224
	MsgFriendListOK  uint16 = 225
	MsgProfileReq    uint16 = 230
	MsgProfileResp   uint16 = 231
	MsgSetNickname   uint16 = 232 // 设置/修改游戏内昵称
	MsgSetNicknameOK uint16 = 233
	MsgEloUpdate     uint16 = 234 // 服务端推送：对局结算后 ELO 变动
	MsgErrorResp     uint16 = 299
)
