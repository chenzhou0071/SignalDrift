package protocol

// 消息 ID 分配：1-99 网关层，100-199 测试/通用，200-299 大厅（计划3），300-399 房间（计划4）
const (
	MsgHeartbeat    uint16 = 1
	MsgHeartbeatAck uint16 = 2
	MsgEcho         uint16 = 100
)
