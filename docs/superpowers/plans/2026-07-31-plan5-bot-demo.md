# 《信号漂流》计划 5/5：Bot、观战与面试演示包 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 服务端 Bot（与真人同输入通道、状态机贪心、配置分难度）、大厅人机练习入口、Bot vs Bot 观战模式（宣传素材生产线）、Prometheus 指标 + Grafana 监控大屏、docker-compose 一键部署（面试演示包）。

**Architecture:** Bot 实现为 `internal/bot` 纯决策包：输入 `*battle.World` 只读视图 + 自己 slot，输出 `battle.PlayerInput`——由 Room 在 Tick 前调用注入输入队列，与真人指令完全同通道。观战者是房间的"只收不发"成员。指标用 prometheus client 暴露 /metrics。

**Tech Stack:** Go 1.22+；新增依赖仅 `github.com/prometheus/client_golang`；Docker/docker-compose；Grafana+Prometheus 官方镜像。

**前置:** 计划 1-4 已完成（完整可对局）。

## Global Constraints

- Bot 无任何后门：不直接改 World，只产出 PlayerInput 走 Room 输入队列（含发射限流同样生效）
- Bot 决策参数全部来自 `configs/bot.json`，三档难度 easy/normal/hard
- 观战者不参与 Step，仅接收 Snapshot+State 广播；观战消息复用战斗 msgID，新增 `MsgSpectateJoin=305`
- 指标端口：gateway :9100、roomd :9101（http /metrics）
- 每 Task `go test ./... -race` 全绿再提交

## File Structure

```
server/
  configs/bot.json
  internal/bot/bot.go               — 决策状态机
  internal/bot/scan.go              — 地图空白区扫描
  internal/room/room.go             — 修改：Bot 座位与观战者列表
  internal/lobby/service.go         — 修改：人机练习/观战 handler
  internal/metrics/metrics.go       — 指标定义与 HTTP 暴露
  deploy/Dockerfile.gateway / Dockerfile.roomd
  deploy/docker-compose.yml
  deploy/prometheus.yml
  deploy/grafana-dashboard.json
SignalDrift/Assets/Scripts/
  UI/LobbyController.cs             — 修改：人机练习/观战按钮
  Game/BattleController.cs          — 修改：观战模式（不发输入）
```

---

### Task 1: 空白区扫描（Bot 的"眼睛"）

**Files:**
- Create: `server/internal/bot/scan.go`
- Test: `server/internal/bot/scan_test.go`

**Interfaces:**
- Produces: `bot.BestTarget(g *battle.Grid, mySlot uint8, fromX, fromY, maxRange float64) (x, y float64, ok bool)`——把地图划成 8×8 格的粗块（16×9 块），统计每块内"非己方色可涂格"数，返回射程内得分最高块的中心；全为己方色返回 ok=false

- [ ] **Step 1: 写失败测试**

```go
package bot

import (
	"testing"

	"signaldrift/server/internal/battle"
)

func TestBestTargetPrefersBlank(t *testing.T) {
	cfg, m, err := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	if err != nil {
		t.Fatal(err)
	}
	w := battle.NewWorld(cfg, m, 1)
	// 把左半图涂成己方色，目标应指向右侧空白
	for j := 0; j < 72; j++ {
		for i := 0; i < 60; i++ {
			w.Grid.Paint(j*128+i, battle.CellP0)
		}
	}
	x, _, ok := BestTarget(w.Grid, battle.CellP0, 300, 360, 10000)
	if !ok || x < 640 {
		t.Fatalf("x=%f ok=%v", x, ok)
	}
}

func TestBestTargetRespectsRange(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	w := battle.NewWorld(cfg, m, 1)
	x, y, ok := BestTarget(w.Grid, battle.CellP0, 100, 360, 200)
	if !ok {
		t.Fatal("must find target")
	}
	if dx, dy := x-100, y-360; dx*dx+dy*dy > 210*210 { // 允许块中心略超
		t.Fatalf("out of range %f,%f", x, y)
	}
}
```

- [ ] **Step 2: 确认失败 → 实现**

```go
package bot

import "signaldrift/server/internal/battle"

const blockCells = 8

// BestTarget 扫描粗块，返回射程内"可争夺格"最多的块中心。
// mySlot 传颜色常量（CellP0/CellP1）。
func BestTarget(g *battle.Grid, myColor uint8, fromX, fromY, maxRange float64) (float64, float64, bool) {
	bw, bh := g.W/blockCells, g.H/blockCells // 16×9 块
	bestScore, bestX, bestY := 0, 0.0, 0.0
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			cx := (float64(bx*blockCells) + float64(blockCells)/2) * g.CellSize
			cy := (float64(by*blockCells) + float64(blockCells)/2) * g.CellSize
			dx, dy := cx-fromX, cy-fromY
			if dx*dx+dy*dy > maxRange*maxRange {
				continue
			}
			score := 0
			for j := by * blockCells; j < (by+1)*blockCells; j++ {
				for i := bx * blockCells; i < (bx+1)*blockCells; i++ {
					idx := j*g.W + i
					if g.PaintableAt(idx) && g.ColorAtIdx(idx) != myColor {
						score++
					}
				}
			}
			if score > bestScore {
				bestScore, bestX, bestY = score, cx, cy
			}
		}
	}
	return bestX, bestY, bestScore > 0
}
```

需要给 Grid 补两个只读方法（加入本 Task）：`PaintableAt(idx int) bool`（MaskFree）、`ColorAtIdx(idx int) uint8`，附带各一个单测断言。

- [ ] **Step 3: 全绿提交** `feat(bot): 粗块空白区扫描`

---

### Task 2: Bot 决策状态机

**Files:**
- Create: `server/configs/bot.json`、`server/internal/bot/bot.go`
- Test: `server/internal/bot/bot_test.go`

**Interfaces:**
- Produces:

```go
type Difficulty struct {
	AimJitterDeg   float64 `json:"aim_jitter_deg"`   // 瞄准误差角
	ReactionTicks  int     `json:"reaction_ticks"`   // 决策间隔
	RefillBelow    float64 `json:"refill_below"`     // 低于此墨量回撤
	HarassRange    float64 `json:"harass_range"`     // 骚扰射程
}
func LoadBotConfig(path string) (map[string]Difficulty, error) // easy/normal/hard
type Bot struct{ /* slot, diff, rng, state, 内部计时 */ }
func NewBot(slot uint8, diff Difficulty, seed int64) *Bot
func (b *Bot) Decide(w *battle.World) battle.PlayerInput  // 每 Tick 调用；非决策帧重复上次输入
```

`bot.json`：

```json
{
  "easy":   { "aim_jitter_deg": 20, "reaction_ticks": 30, "refill_below": 20, "harass_range": 250 },
  "normal": { "aim_jitter_deg": 10, "reaction_ticks": 15, "refill_below": 20, "harass_range": 320 },
  "hard":   { "aim_jitter_deg": 4,  "reaction_ticks": 8,  "refill_below": 15, "harass_range": 400 }
}
```

状态机（每 reaction_ticks 重估一次，优先级从高到低）：
1. **AVOID**：与黑洞距离 < effect_radius×1.2 → 反向移动分量
2. **REFILL**：墨量 < refill_below → 朝最近己方色块移动（扫描同 Task 1 粗块中"己方色最多且最近"），不开火
3. **HARASS**：敌人在线且距离 < harass_range → 朝敌人当前位置直射（加抖动角），移动方向绕敌侧移
4. **PAINT**（默认）：`BestTarget` 找目标块——距离 > lob 射程×0.8 用抛射砸，否则移动过去+直射铺路

瞄准抖动：目标向量旋转 `rng.NormFloat64()*jitter` 度。

- [ ] **Step 1: 写失败测试**

```go
func TestBotRefillWhenLowInk(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	w := battle.NewWorld(cfg, m, 1)
	b := NewBot(0, Difficulty{ReactionTicks: 1, RefillBelow: 20, HarassRange: 300}, 1)
	w.Players[0].Ink = 5
	w.Players[0].X, w.Players[0].Y = 640, 360 // 远离己方色
	in := b.Decide(w)
	if in.FireStraight || in.FireLob {
		t.Fatal("low ink must not fire")
	}
	if in.MoveX == 0 && in.MoveY == 0 {
		t.Fatal("must move toward own paint")
	}
}

func TestBotPaintsWhenIdle(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	w := battle.NewWorld(cfg, m, 1)
	w.Players[1].Online = false // 无敌人干扰
	b := NewBot(0, Difficulty{ReactionTicks: 1, RefillBelow: 20, HarassRange: 300}, 1)
	in := b.Decide(w)
	if !in.FireStraight && !in.FireLob {
		t.Fatal("full ink idle bot must be painting")
	}
}

func TestBotHarassesNearbyEnemy(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	w := battle.NewWorld(cfg, m, 1)
	w.Players[0].X, w.Players[0].Y = 600, 360
	w.Players[1].X, w.Players[1].Y = 700, 360
	b := NewBot(0, Difficulty{ReactionTicks: 1, RefillBelow: 20, HarassRange: 300, AimJitterDeg: 0}, 1)
	in := b.Decide(w)
	if !in.FireStraight {
		t.Fatal("must harass")
	}
	if in.AimX < 650 { // 朝敌方向
		t.Fatalf("aim=%f", in.AimX)
	}
}

func TestBotDeterministicWithSeed(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	w1 := battle.NewWorld(cfg, m, 1)
	w2 := battle.NewWorld(cfg, m, 1)
	b1 := NewBot(0, Difficulty{ReactionTicks: 1, AimJitterDeg: 10, RefillBelow: 20, HarassRange: 300}, 7)
	b2 := NewBot(0, Difficulty{ReactionTicks: 1, AimJitterDeg: 10, RefillBelow: 20, HarassRange: 300}, 7)
	if b1.Decide(w1) != b2.Decide(w2) {
		t.Fatal("same seed must be deterministic")
	}
}
```

- [ ] **Step 2: 确认失败 → 实现状态机**（Decide 内部：计时未到 reaction_ticks 返回缓存输入；到期按优先级重估；移动向量归一化×100 转 int8）
- [ ] **Step 3: 全绿提交** `feat(bot): 决策状态机——回墨/骚扰/涂色/避险与难度参数`

---

### Task 3: Room 接入 Bot 与观战者

**Files:**
- Modify: `server/internal/room/room.go`、`server/internal/room/manager.go`
- Test: `server/internal/room/bot_room_test.go`

**Interfaces:**
- `NewRoom(...)` 追加可选项 `WithBot(slot uint8, b *bot.Bot)`（函数式 Option）；Bot 座位：Tick 前调用 `b.Decide(world)` 把输入写入该 slot 输入槽（走同一 `applyInput` 路径，发射限流同样生效）；Bot 座位视为永远 Online（不触发断线判负）、不占 Sink 推送
- 观战：`(*Room).Spectate(uid int64)`（发 Snapshot，加入广播列表）、`(*Room).Unspectate(uid int64)`；`MsgRoomCreate` body 追加 1 字节 `mode`（0 双人 / 1 单人+Bot / 2 双 Bot 观战），uidB 在 mode!=0 时为难度档（0/1/2 → easy/normal/hard）

- [ ] **Step 1: 写失败测试**

```go
func TestBotRoomRunsToSettle(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	bots, _ := bot.LoadBotConfig("../../configs/bot.json")
	sink := &fakeSink{}
	var res *battle.Result
	r := NewRoom(1, [2]int64{10, -1}, cfg, m, sink,
		func(rr *battle.Result, w *battle.World, uids [2]int64) { res = rr },
		WithBot(1, bot.NewBot(1, bots["normal"], 1)))
	r.StartManual()
	r.Join(10)
	// 人类挂机，Bot 应最终以覆盖率或时限取胜
	for i := 0; i < cfg.MatchDurationTicks+cfg.WinCountdownTicks+2 && res == nil; i++ {
		r.TickOnce()
	}
	if res == nil {
		t.Fatal("match must settle")
	}
	if res.Cov[1] <= res.Cov[0] {
		t.Fatalf("idle human should lose coverage: %v", res.Cov)
	}
}

func TestSpectatorReceivesBroadcast(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	bots, _ := bot.LoadBotConfig("../../configs/bot.json")
	sink := &fakeSink{}
	r := NewRoom(1, [2]int64{-1, -2}, cfg, m, sink, func(*battle.Result, *battle.World, [2]int64) {},
		WithBot(0, bot.NewBot(0, bots["normal"], 1)), WithBot(1, bot.NewBot(1, bots["normal"], 2)))
	r.StartManual()
	r.Spectate(99)
	r.TickOnce()
	if countMsgFor(sink, 99, protocol.MsgBattleState) != 1 {
		t.Fatal("spectator must receive state")
	}
}
```

- [ ] **Step 2: 实现**（Bot 座位在 waiting 判定中视为已 Join；双 Bot 房 Spectate 到来才开始 Step 或直接开跑——决策：**直接开跑**，观战中途加入靠快照）
- [ ] **Step 3: 全绿提交** `feat(room): Bot座位与观战广播`

---

### Task 4: 大厅入口与 Unity 按钮

**Files:**
- Modify: `server/internal/lobby/service.go`（+`server/internal/protocol/msgid.go`：`MsgPracticeReq=240/MsgPracticeResp=241`、`MsgSpectateReq=242/MsgSpectateResp=243`）
- Modify: `Assets/Scripts/UI/LobbyController.cs`、`Assets/Scripts/Game/BattleController.cs`

**Interfaces:**
- `MsgPracticeReq` body JSON `{"difficulty":"easy|normal|hard"}` → 大厅走 RoomAllocator 同路径建 mode=1 房间 → 回 `MsgPracticeResp{code, room_id}` → 客户端照常 MsgRoomJoin
- `MsgSpectateReq`（空 body）→ 建 mode=2 双 Bot 房 → 回 room_id → 客户端 `MsgSpectateJoin{room_id}`（网关同样转发）→ BattleController 观战模式：不发输入、HUD 显示"观战中"、结算后自动再开一局按钮
- Unity 大厅加两按钮：【人机练习▾】（难度三选）、【观战 Bot 对局】；匹配超时 60 秒弹窗"是否转人机练习？"

- [ ] **Step 1: 大厅 handler 单测**（复用计划 2 testService 骨架：发 PracticeReq → 收 room_id ≠ 0；allocator stub 记录 mode 参数正确）
- [ ] **Step 2: 实现服务端 + Unity 按钮与观战模式**
- [ ] **Step 3: 冒烟**：单客户端人机练习完整一局（hard 档应明显压制挂机玩家）；观战 Bot vs Bot 一局并录屏 30 秒——**这段录屏就是第一份宣传素材**
- [ ] **Step 4: 双仓库提交** `feat: 人机练习与Bot观战全链路`

---

### Task 5: Prometheus 指标

**Files:**
- Create: `server/internal/metrics/metrics.go`
- Modify: `cmd/gateway/main.go`、`cmd/roomd/main.go`、`internal/gateway/server.go`、`internal/room/room.go`（埋点）
- Test: `server/internal/metrics/metrics_test.go`

**Interfaces:**
- Produces（规格 6.1 指标清单落地）:

```go
metrics.Serve(addr string)                    // 起 /metrics HTTP
// 网关：GaugeOnlineConns、CounterPacketsIn/Out、CounterRateLimitKick、CounterDisconnects
// 大厅：GaugeMatchPoolSize、CounterLogins、CounterMatchesMade
// 房间：GaugeActiveRooms、HistogramTickDuration(buckets 0.1ms~33ms)、
//       CounterFireCmds、GaugeDirtyCellsPerTick、CounterBroadcastBytes
```

- [ ] **Step 1: 测试**（`promhttp` 测试服务器抓 /metrics 文本，断言注册的指标名出现；Tick 埋点用 room 手动 TickOnce 后 Histogram 计数 >0）
- [ ] **Step 2: 实现 + 各进程埋点**（room Tick 包裹 `t0:=time.Now()` … `TickDuration.Observe(time.Since(t0).Seconds())`；广播处累加 bytes；网关读循环累加 PacketsIn）
- [ ] **Step 3: 冒烟**：跑一局人机，`curl :9101/metrics | findstr tick_duration` 有分布数据
- [ ] **Step 4: 提交** `feat(metrics): Prometheus全链路指标`

---

### Task 6: docker-compose 演示包

**Files:**
- Create: `server/deploy/Dockerfile.gateway`、`Dockerfile.roomd`、`docker-compose.yml`、`prometheus.yml`、`grafana-dashboard.json`、`deploy/README.md`

- [ ] **Step 1: Dockerfile**（多阶段构建，二进制+configs 拷入 alpine；两份仅 CMD 不同）

```dockerfile
# Dockerfile.gateway
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /out/gateway ./cmd/gateway
FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/gateway .
COPY configs ./configs
EXPOSE 8080 8090 9100
CMD ["./gateway"]
```

- [ ] **Step 2: docker-compose.yml**

```yaml
services:
  mysql:
    image: mysql:8.4
    environment: { MYSQL_ROOT_PASSWORD: root, MYSQL_DATABASE: signaldrift }
    volumes: [ "../internal/store/schema.sql:/docker-entrypoint-initdb.d/schema.sql" ]
    healthcheck: { test: ["CMD","mysqladmin","ping","-proot"], interval: 5s, retries: 10 }
  gateway:
    build: { context: .., dockerfile: deploy/Dockerfile.gateway }
    ports: [ "8080:8080", "9100:9100" ]
    depends_on: { mysql: { condition: service_healthy } }
  roomd1:
    build: { context: .., dockerfile: deploy/Dockerfile.roomd }
    depends_on: [ gateway ]
  roomd2:
    build: { context: .., dockerfile: deploy/Dockerfile.roomd }
    depends_on: [ gateway ]
  prometheus:
    image: prom/prometheus
    volumes: [ "./prometheus.yml:/etc/prometheus/prometheus.yml" ]
    ports: [ "9090:9090" ]
  grafana:
    image: grafana/grafana
    ports: [ "3000:3000" ]
```

（配置注意：容器内 lobby.json 的 DSN 指向 `mysql:3306`、roomd.json 的 gateway_addr 指向 `gateway:8090`——用环境变量覆盖或提供 `configs.docker/` 目录，决策：**提供 configs.docker 目录**，Dockerfile 各自 COPY 对应目录。）

- [ ] **Step 3: grafana-dashboard.json**：四行面板——在线连接/活跃房间、Tick 耗时 P50/P99、每秒发射与广播带宽、限流踢出与断线。README 写导入步骤（Grafana 添加 Prometheus 数据源 `http://prometheus:9090` → Import json）
- [ ] **Step 3b: 告警规则**（规格 6.1"告警阈值"）：`deploy/alert.rules.yml` 定义 4 条 Prometheus 告警——Tick 耗时 P99>20ms（卡顿）、限流踢出速率突增（恶意发包）、事件队列积压>阈值（DB IO 阻塞）、房间负载>90%（扩容提醒）；prometheus.yml 挂载该 rules 文件，Grafana 面板叠加告警状态
- [ ] **Step 4: 验收**：`docker compose up -d` → 本机 Unity 客户端连 `127.0.0.1:8080` 打一局人机 → Grafana 大屏指标全部在动 → **截图存入 `docs/media/`（简历/面试素材）**
- [ ] **Step 5: 提交** `feat(deploy): docker-compose一键演示包/Grafana大屏/告警规则`

---

### Task 7: 收尾——README 与宣传素材清单

**Files:**
- Create: `server/README.md`（架构图 ASCII、技术亮点清单、快速启动、协议文档表）
- Create: `docs/media/`（素材目录）

- [ ] **Step 1: README**：三层架构图、消息 ID 总表、"技术亮点"章节按面试话术组织（服务端权威模拟/增量同步+RLE/断线重连/负载均衡/异步队列/防作弊清单/压测数据占位）
- [ ] **Step 2: 素材生产**：观战模式录 60 秒 Bot 对局（OBS）；结算面板、Grafana 大屏、匹配流程各截图 1 张
- [ ] **Step 3: 提交** `docs: README与演示素材`

---

## Self-Review 结果

1. **规格覆盖**（规格第九章全部）：Bot 同通道无后门(T3)、贪心状态机四行为(T2)、难度纯配置(T2)、人机练习入口+匹配超时转人机(T4)、Bot vs Bot+观战(T3,T4)、录屏素材(T4,T7)、docker-compose 云端/单机双形态(T6)、Grafana 大屏(T5,T6)。规格 6.1 监控指标清单在 T5 逐项落地。
2. **占位符扫描**：无 TBD；"压测数据占位"是 README 中的预留章节标题（后续用真实压测填充），非实现缺口。
3. **类型一致性**：`bot.Bot.Decide(w) battle.PlayerInput` 与 Room 输入槽同型；MsgRoomCreate 的 mode 字节扩展向后兼容（计划 4 实现时 body 定长校验需允许 +1 字节——执行计划 4 时直接按含 mode 的最终布局实现）。
```
