# 《信号漂流》计划 4/5：房间战斗服务与战斗客户端 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 战斗核心模拟库（格子涂色/双弹道/黑洞反射/墨量/胜负倒计时）+ 独立 roomd 房间进程（注册中心/负载分配/会话转发）+ 30Hz Tick 房间 + 脏格子增量与 RLE 快照同步 + 断线重连 + Unity 战斗场景（输入流/插值/涂色贴图/HUD/结算）。

**Architecture:** `internal/battle` 为纯逻辑包（零网络零 IO，确定性可测，rng/时钟注入）；`cmd/roomd` 独立进程通过内部 TCP（复用 protocol 帧）连接网关注册并收发"会话标记转发帧"；网关新增 ForwardTable 与 RoomdRegistry；大厅的 RoomAllocator 注入点接真实分配。战斗同步走二进制（非 JSON）。

**Tech Stack:** Go 1.22+ 标准库；Unity 侧沿用计划 3 地基。

**前置:** 计划 1、2、3 已完成。

## Global Constraints

- 战斗消息 msgID：客户端↔房间 300-349；网关↔roomd 内部 400-449
- 战斗包体一律**二进制大端序**（涂色数据量大，JSON 不可接受）；坐标 float32、格子索引 uint16（低 14 位 idx + 高 2 位颜色）
- 地图 128×72 格、格长 10、世界 1280×720；玩家半径 15；全部数值以 `configs/battle.json`、`configs/map_01.json` 为唯一事实源
- battle 包禁止 import net/database/log 之外注入不了的副作用；随机数用注入的 `*rand.Rand`
- 房间 goroutine 永不碰 MySQL；结算经 MsgRoomResult 回传网关进程入异步队列
- Tick=30Hz（dt=1/30s）；对局 180s=5400 Tick；倒计时 5s=150 Tick；重连缓冲 30s=900 Tick
- 每 Task `go test ./... -race` 全绿再提交

## File Structure

```
server/
  configs/battle.json / map_01.json
  internal/battle/config.go        — 战斗与地图配置结构体+加载
  internal/battle/grid.go          — 格子矩阵/涂色/覆盖率/脏集合/RLE快照
  internal/battle/painter.go       — 溅射圆与走廊涂色（概率模型）
  internal/battle/world.go         — World/Player/Projectile 与 Step 主循环
  internal/battle/projectile.go    — 弹体模拟（直射/抛射/黑洞/反射/墙）
  internal/battle/win.go           — 覆盖统计/倒计时/胜负（并入 world.go 亦可，拆文件保持聚焦）
  internal/battle/codec.go         — 状态包/快照/输入的二进制编解码
  internal/room/room.go            — 房间 goroutine：输入队列/Tick/加入/重连/结算
  internal/room/manager.go         — roomd 内房间管理与负载统计
  internal/gateway/forward.go      — ForwardTable + RoomdRegistry（网关侧）
  internal/protocol/msgid.go       — 追加战斗与内部消息 ID
  cmd/roomd/main.go                — 房间进程入口
SignalDrift/Assets/Scripts/
  Game/BattleController.cs         — 战斗场景总控（入房/输入/状态应用）
  Game/PaintRenderer.cs            — 涂色贴图渲染
  Game/EntityView.cs               — 玩家/弹体插值视图
  Game/BattleCodec.cs              — C# 二进制编解码（对齐 codec.go）
  UI/BattleHud.cs / SettlePanel.cs — HUD 与结算面板
```

---

### Task 1: 战斗/地图配置

**Files:**
- Create: `server/configs/battle.json`、`server/configs/map_01.json`、`server/internal/battle/config.go`
- Test: `server/internal/battle/config_test.go`

**Interfaces:**
- Produces: `battle.LoadConfig(battlePath, mapPath) (*Config, *MapDef, error)` 及结构体（字段即 json，全部导出）

- [ ] **Step 1: 配置文件**

`battle.json`（首版数值，调平衡只改此文件）：

```json
{
  "tick_rate": 30,
  "match_duration_ticks": 5400,
  "win_ratio": 0.75,
  "win_countdown_ticks": 150,
  "reconnect_grace_ticks": 900,
  "settle_destroy_ticks": 300,
  "player": { "radius": 15, "base_speed": 240,
    "own_speed_mult": 1.3, "enemy_speed_mult": 0.7,
    "own_regen_mult": 2.0, "enemy_regen_mult": 0.4 },
  "ink": { "max": 100, "regen_per_sec": 10, "straight_cost": 3, "lob_cost": 25 },
  "straight": { "speed": 600, "max_range": 400, "radius": 4,
    "fire_interval_ticks": 4, "trail_side_prob": 0.6,
    "end_splat_radius": 30, "hit_slow_ticks": 45, "hit_slow_mult": 0.7 },
  "lob": { "speed": 300, "max_range": 550, "radius": 6,
    "fire_interval_ticks": 20, "splat_radius": 60,
    "core_ratio": 0.6, "mid_ratio": 0.85, "mid_prob": 0.5, "edge_prob": 0.2 },
  "reflect_range_cost": 60,
  "blackhole": { "effect_radius": 120, "kill_radius": 15,
    "proj_gravity": 900, "player_pull": 120 },
  "fire_rate": { "rate": 10, "burst": 15 }
}
```

`map_01.json`（对称图：中央黑洞+侧翼墙+反射墙+主塔）：

```json
{
  "width_cells": 128, "height_cells": 72, "cell_size": 10,
  "towers": [ { "x": 80, "y": 360, "safe_radius": 60 },
              { "x": 1200, "y": 360, "safe_radius": 60 } ],
  "walls": [
    { "x": 400, "y": 130, "w": 130, "h": 22, "reflect": false },
    { "x": 750, "y": 568, "w": 130, "h": 22, "reflect": false },
    { "x": 300, "y": 300, "w": 22, "h": 120, "reflect": false },
    { "x": 958, "y": 300, "w": 22, "h": 120, "reflect": false },
    { "x": 560, "y": 80,  "w": 90,  "h": 16, "reflect": true },
    { "x": 630, "y": 624, "w": 90,  "h": 16, "reflect": true }
  ],
  "blackholes": [ { "x": 640, "y": 360 } ]
}
```

- [ ] **Step 2: 失败测试 → 实现 → 提交**（模式同计划 2 Task 1：临时目录写样例 json，断言关键字段；`LoadConfig` 双文件读入+Unmarshal，校验 `width_cells*height_cells<=16384`（idx 要装进 14 位）否则报错）

```bash
git -C "E:\pro\SignalDrift" add server
git -C "E:\pro\SignalDrift" commit -m "feat(battle): 战斗与地图配置定义与加载"
```

---

### Task 2: Grid——涂色/覆盖率/脏集合/RLE 快照

**Files:**
- Create: `server/internal/battle/grid.go`
- Test: `server/internal/battle/grid_test.go`

**Interfaces:**
- Produces:

```go
const (CellNone uint8=0; CellP0=1; CellP1=2)                 // 颜色
const (MaskFree uint8=0; MaskWall=1; MaskNoPaint=2; MaskPerm=3) // 静态掩码
type Grid struct{ W,H int; CellSize float64 /*内部: colors,mask []uint8; cnt [2]int; paintable int; dirty []uint16 */ }
func NewGrid(m *MapDef) *Grid                    // 由地图构建掩码：墙格、黑洞禁涂圈(effect_radius)、主塔永久色圈
func (g *Grid) IdxAt(x,y float64) (int, bool)    // 越界 false
func (g *Grid) ColorAt(x,y float64) uint8
func (g *Grid) Paint(idx int, c uint8) bool      // Mask 非 Free 拒绝；变更才记 dirty/更新计数
func (g *Grid) Coverage() (c0,c1 float64)        // cnt/paintable（永久格计入，见规格2.5）
func (g *Grid) DrainDirty() []uint16             // 取走并清空本帧脏集合（含颜色打包：idx | color<<14）
func (g *Grid) Snapshot() []byte                 // RLE: 重复 (runLen uint16, color uint8)
func DecodeSnapshot(data []byte, w,h int) ([]uint8, error)
```

- [ ] **Step 1: 写失败测试**

```go
package battle

import "testing"

func testMap() *MapDef {
	return &MapDef{WidthCells: 128, HeightCells: 72, CellSize: 10,
		Towers: []TowerDef{{X: 80, Y: 360, SafeRadius: 60}, {X: 1200, Y: 360, SafeRadius: 60}},
		Walls:  []WallDef{{X: 400, Y: 130, W: 130, H: 22}},
		Blackholes: []BlackholeDef{{X: 640, Y: 360}},
	}
}

func TestPaintAndCoverage(t *testing.T) {
	g := NewGrid(testMap())
	idx, ok := g.IdxAt(15, 15)
	if !ok {
		t.Fatal("in bounds")
	}
	if !g.Paint(idx, CellP0) {
		t.Fatal("paint free cell must succeed")
	}
	if g.Paint(idx, CellP0) {
		t.Fatal("same color repaint must be no-op false")
	}
	c0, _ := g.Coverage()
	if c0 <= 0 {
		t.Fatal("coverage must rise")
	}
	if g.ColorAt(15, 15) != CellP0 {
		t.Fatal("ColorAt")
	}
}

func TestWallAndNoPaintRejected(t *testing.T) {
	g := NewGrid(testMap())
	wallIdx, _ := g.IdxAt(405, 135) // 墙内
	if g.Paint(wallIdx, CellP0) {
		t.Fatal("wall cell must reject")
	}
	bhIdx, _ := g.IdxAt(640, 360) // 黑洞禁涂圈
	if g.Paint(bhIdx, CellP0) {
		t.Fatal("nopaint cell must reject")
	}
}

func TestTowerPermanentColor(t *testing.T) {
	g := NewGrid(testMap())
	idx, _ := g.IdxAt(80, 360)
	if g.ColorAt(80, 360) != CellP0 {
		t.Fatal("tower zone pre-painted P0")
	}
	if g.Paint(idx, CellP1) {
		t.Fatal("permanent cell must reject")
	}
	c0, c1 := g.Coverage()
	if c0 != c1 { // 对称塔区，初始覆盖相等
		t.Fatalf("c0=%f c1=%f", c0, c1)
	}
}

func TestDirtyDrain(t *testing.T) {
	g := NewGrid(testMap())
	idx, _ := g.IdxAt(15, 15)
	g.Paint(idx, CellP1)
	d := g.DrainDirty()
	if len(d) != 1 || int(d[0]&0x3FFF) != idx || uint8(d[0]>>14) != CellP1 {
		t.Fatalf("d=%v", d)
	}
	if len(g.DrainDirty()) != 0 {
		t.Fatal("drained must be empty")
	}
}

func TestSnapshotRoundtrip(t *testing.T) {
	g := NewGrid(testMap())
	i1, _ := g.IdxAt(15, 15)
	i2, _ := g.IdxAt(500, 500)
	g.Paint(i1, CellP0)
	g.Paint(i2, CellP1)
	colors, err := DecodeSnapshot(g.Snapshot(), 128, 72)
	if err != nil || len(colors) != 128*72 {
		t.Fatalf("err=%v", err)
	}
	if colors[i1] != CellP0 || colors[i2] != CellP1 {
		t.Fatal("roundtrip mismatch")
	}
}
```

- [ ] **Step 2: 确认失败 → 实现核心**（关键方法）

```go
func (g *Grid) Paint(idx int, c uint8) bool {
	if idx < 0 || idx >= len(g.colors) || g.mask[idx] != MaskFree || g.colors[idx] == c {
		return false
	}
	old := g.colors[idx]
	if old == CellP0 { g.cnt[0]-- } else if old == CellP1 { g.cnt[1]-- }
	if c == CellP0 { g.cnt[0]++ } else if c == CellP1 { g.cnt[1]++ }
	g.colors[idx] = c
	g.dirty = append(g.dirty, uint16(idx)|uint16(c)<<14)
	return true
}

func (g *Grid) Snapshot() []byte {
	var out []byte
	i := 0
	for i < len(g.colors) {
		j := i
		for j < len(g.colors) && g.colors[j] == g.colors[i] && j-i < 0xFFFF {
			j++
		}
		out = append(out, byte((j-i)>>8), byte(j-i), g.colors[i])
		i = j
	}
	return out
}
```

`NewGrid`：遍历格子中心点，命中墙矩形→MaskWall；距黑洞 ≤ effect_radius→MaskNoPaint（此处硬编码用 MapDef 内嵌半径 120——**修正**：把 `nopaint_radius` 加进 `BlackholeDef` json 字段，默认 120，保持配置驱动）；距塔 ≤ safe_radius→MaskPerm 且 colors=塔序号色、计入 cnt/paintable。`paintable` = MaskFree+MaskPerm 格数。

- [ ] **Step 3: 全绿提交**

```bash
git -C "E:\pro\SignalDrift" commit -am "feat(battle): Grid涂色矩阵/覆盖率/脏集合/RLE快照"
```

---

### Task 3: Painter——溅射圆与走廊涂色

**Files:**
- Create: `server/internal/battle/painter.go`
- Test: `server/internal/battle/painter_test.go`

**Interfaces:**
- Produces: `battle.SplatCircle(g *Grid, cx,cy,r float64, c uint8, lob *LobConfig, rng *rand.Rand) int`（按 core/mid/edge 三层概率涂色，返回改动格数）；`battle.PaintCorridor(g *Grid, x0,y0,x1,y1 float64, c uint8, sideProb float64, rng *rand.Rand)`（沿线段步进 cellSize/2，中心格必涂、左右邻格按 sideProb）

- [ ] **Step 1: 失败测试**（要点：固定 seed 的 rng）

```go
func TestSplatThreeRings(t *testing.T) {
	g := NewGrid(testMap())
	lob := &LobConfig{SplatRadius: 60, CoreRatio: 0.6, MidRatio: 0.85, MidProb: 0.5, EdgeProb: 0.2}
	rng := rand.New(rand.NewSource(1))
	n := SplatCircle(g, 900, 200, lob.SplatRadius, CellP0, lob, rng)
	if n < 60 || n > 130 { // 半径6格理论上限约113格
		t.Fatalf("n=%d", n)
	}
	// 核心区(≤36u)必为实心
	if g.ColorAt(900, 200) != CellP0 || g.ColorAt(925, 200) != CellP0 {
		t.Fatal("core must be solid")
	}
}

func TestSplatSkipsWall(t *testing.T) {
	g := NewGrid(testMap())
	lob := &LobConfig{SplatRadius: 60, CoreRatio: 0.6, MidRatio: 0.85, MidProb: 0.5, EdgeProb: 0.2}
	SplatCircle(g, 405, 135, 60, CellP0, lob, rand.New(rand.NewSource(1)))
	if g.ColorAt(405, 135) != CellNone { // 墙格 ColorAt 返回 None
		t.Fatal("wall must stay unpainted")
	}
}

func TestCorridorPaintsLine(t *testing.T) {
	g := NewGrid(testMap())
	PaintCorridor(g, 100, 100, 300, 100, CellP1, 0.6, rand.New(rand.NewSource(1)))
	for x := 105.0; x < 300; x += 10 {
		if g.ColorAt(x, 100) != CellP1 {
			t.Fatalf("center line gap at %f", x)
		}
	}
}
```

- [ ] **Step 2: 实现**

```go
func SplatCircle(g *Grid, cx, cy, r float64, c uint8, lob *LobConfig, rng *rand.Rand) int {
	n := 0
	cs := g.CellSize
	minI, maxI := int((cx-r)/cs), int((cx+r)/cs)
	minJ, maxJ := int((cy-r)/cs), int((cy+r)/cs)
	for j := minJ; j <= maxJ; j++ {
		for i := minI; i <= maxI; i++ {
			if i < 0 || i >= g.W || j < 0 || j >= g.H {
				continue
			}
			px, py := (float64(i)+0.5)*cs, (float64(j)+0.5)*cs
			d := math.Hypot(px-cx, py-cy)
			if d > r {
				continue
			}
			p := 1.0
			switch {
			case d <= r*lob.CoreRatio: // 100%
			case d <= r*lob.MidRatio:
				p = lob.MidProb
			default:
				p = lob.EdgeProb
			}
			if p >= 1.0 || rng.Float64() < p {
				if g.Paint(j*g.W+i, c) {
					n++
				}
			}
		}
	}
	return n
}

func PaintCorridor(g *Grid, x0, y0, x1, y1 float64, c uint8, sideProb float64, rng *rand.Rand) {
	dist := math.Hypot(x1-x0, y1-y0)
	if dist == 0 {
		return
	}
	step := g.CellSize / 2
	dx, dy := (x1-x0)/dist, (y1-y0)/dist
	nx, ny := -dy, dx // 法向
	for s := 0.0; s <= dist; s += step {
		px, py := x0+dx*s, y0+dy*s
		if idx, ok := g.IdxAt(px, py); ok {
			g.Paint(idx, c)
		}
		for _, side := range []float64{-1, 1} {
			if rng.Float64() < sideProb {
				if idx, ok := g.IdxAt(px+nx*side*g.CellSize, py+ny*side*g.CellSize); ok {
					g.Paint(idx, c)
				}
			}
		}
	}
}
```

- [ ] **Step 3: 全绿提交** `feat(battle): 溅射浓度模型与走廊涂色`

---

### Task 4: World——玩家移动/踩色/墨量/黑洞拖拽

**Files:**
- Create: `server/internal/battle/world.go`
- Test: `server/internal/battle/world_test.go`

**Interfaces:**
- Produces:

```go
type PlayerInput struct{ MoveX, MoveY int8; FireStraight, FireLob bool; AimX, AimY float64 }
type Player struct{ Slot uint8; UID int64; X,Y float64; Ink float64; SlowTicks int
	Online bool; OfflineTicks int; LastFireTick [2]int
	Stats struct{ PaintedCells, StraightShots, LobShots, Hits, BlackholeLost, Reflects int } }
type World struct{ Cfg *Config; Map *MapDef; Grid *Grid; Players [2]*Player
	Projectiles []*Projectile; Tick int; rng *rand.Rand
	Countdown struct{ Leader uint8; Ticks int } // Leader 0xFF=无
	Result *Result }
type Result struct{ WinnerSlot uint8; Draw bool; Cov [2]float64; DurationTicks int } // WinnerSlot 0xFF=平局
func NewWorld(cfg *Config, m *MapDef, seed int64) *World  // 玩家出生在塔位
func (w *World) Step(inputs [2]PlayerInput)               // 规格5.2顺序：移动→弹体→墨量→覆盖→胜负
func (w *World) speedMult(p *Player) float64              // 踩色倍率×减速debuff
```

- [ ] **Step 1: 失败测试**

```go
func newTestWorld(t *testing.T) *World {
	cfg, m, err := LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	if err != nil {
		t.Fatal(err)
	}
	return NewWorld(cfg, m, 1)
}

func TestSpawnAtTowers(t *testing.T) {
	w := newTestWorld(t)
	if w.Players[0].X != 80 || w.Players[1].X != 1200 {
		t.Fatalf("spawn %f %f", w.Players[0].X, w.Players[1].X)
	}
	if w.Players[0].Ink != 100 {
		t.Fatal("full ink at start")
	}
}

func TestMoveWithOwnPaintBoost(t *testing.T) {
	w := newTestWorld(t)
	p := w.Players[0] // 站在己方永久色上：mult=1.3
	x0 := p.X
	w.Step([2]PlayerInput{{MoveX: 100}, {}})
	moved := p.X - x0
	want := w.Cfg.Player.BaseSpeed * w.Cfg.Player.OwnSpeedMult / float64(w.Cfg.TickRate)
	if math.Abs(moved-want) > 0.01 {
		t.Fatalf("moved=%f want=%f", moved, want)
	}
}

func TestWallBlocksPlayer(t *testing.T) {
	w := newTestWorld(t)
	p := w.Players[0]
	p.X, p.Y = 380, 141 // 墙(400,130,130,22)左侧贴脸
	w.Step([2]PlayerInput{{MoveX: 100}, {}})
	if p.X+w.Cfg.Player.Radius > 400.01 {
		t.Fatalf("penetrated wall x=%f", p.X)
	}
}

func TestInkRegenByFloor(t *testing.T) {
	w := newTestWorld(t)
	p := w.Players[0]
	p.Ink = 50
	w.Step([2]PlayerInput{{}, {}}) // 站己方色：2倍回墨
	wantGain := w.Cfg.Ink.RegenPerSec * w.Cfg.Player.OwnRegenMult / float64(w.Cfg.TickRate)
	if math.Abs(p.Ink-50-wantGain) > 0.001 {
		t.Fatalf("ink=%f", p.Ink)
	}
}

func TestBlackholePullsPlayer(t *testing.T) {
	w := newTestWorld(t)
	p := w.Players[0]
	p.X, p.Y = 640-100, 360 // 引力圈内(effect 120)
	w.Step([2]PlayerInput{{}, {}})
	if p.X <= 540 { // 无输入也被向洞拖动
		t.Fatalf("not pulled x=%f", p.X)
	}
}
```

- [ ] **Step 2: 实现要点**（Step 内玩家子阶段）

```go
func (w *World) stepPlayer(slot int, in PlayerInput) {
	p := w.Players[slot]
	if !p.Online {
		p.OfflineTicks++
		return
	}
	dt := 1.0 / float64(w.Cfg.TickRate)
	sp := w.Cfg.Player.BaseSpeed * w.speedMult(p)
	vx := float64(in.MoveX) / 100 * sp
	vy := float64(in.MoveY) / 100 * sp
	if n := math.Hypot(vx, vy); n > sp { // 斜向不超速
		vx, vy = vx/n*sp, vy/n*sp
	}
	// 黑洞拖拽
	for _, bh := range w.Map.Blackholes {
		d := math.Hypot(p.X-bh.X, p.Y-bh.Y)
		if d < w.Cfg.Blackhole.EffectRadius && d > 1 {
			pull := w.Cfg.Blackhole.PlayerPull * (1 - d/w.Cfg.Blackhole.EffectRadius)
			vx += (bh.X - p.X) / d * pull
			vy += (bh.Y - p.Y) / d * pull
		}
	}
	// 轴分离碰撞：先 X 后 Y，撞墙回退该轴
	nx := clamp(p.X+vx*dt, w.Cfg.Player.Radius, 1280-w.Cfg.Player.Radius)
	if !w.circleHitsWall(nx, p.Y, w.Cfg.Player.Radius) { p.X = nx }
	ny := clamp(p.Y+vy*dt, w.Cfg.Player.Radius, 720-w.Cfg.Player.Radius)
	if !w.circleHitsWall(p.X, ny, w.Cfg.Player.Radius) { p.Y = ny }

	if p.SlowTicks > 0 { p.SlowTicks-- }
	// 回墨
	mult := 1.0
	switch w.Grid.ColorAt(p.X, p.Y) {
	case CellP0 + uint8(slot)*0: // 见实现说明
	}
	// —— 实现说明：own = CellP0+slot? 颜色常量与 slot 对应：slot0→CellP0, slot1→CellP1
	floor := w.Grid.ColorAt(p.X, p.Y)
	own, enemy := CellP0, CellP1
	if slot == 1 { own, enemy = CellP1, CellP0 }
	if floor == own { mult = w.Cfg.Player.OwnRegenMult } else if floor == enemy { mult = w.Cfg.Player.EnemyRegenMult }
	p.Ink = math.Min(w.Cfg.Ink.Max, p.Ink+w.Cfg.Ink.RegenPerSec*mult*dt)
}

func (w *World) speedMult(p *Player) float64 {
	floor := w.Grid.ColorAt(p.X, p.Y)
	own, enemy := CellP0, CellP1
	if p.Slot == 1 { own, enemy = CellP1, CellP0 }
	m := 1.0
	if floor == own { m = w.Cfg.Player.OwnSpeedMult } else if floor == enemy { m = w.Cfg.Player.EnemySpeedMult }
	if p.SlowTicks > 0 { m *= w.Cfg.Straight.HitSlowMult }
	return m
}
```

`circleHitsWall`：圆心到每个墙矩形的最近点距离 < 半径即碰撞。`Step` 总序：`stepPlayer×2 → stepFire×2(Task5) → stepProjectiles(Task5) → stepWin(Task6) → Tick++`。

- [ ] **Step 3: 全绿提交** `feat(battle): World玩家移动/踩色倍率/回墨/黑洞拖拽/墙碰撞`

---

### Task 5: 弹体——发射/直射/抛射/黑洞/反射

**Files:**
- Create: `server/internal/battle/projectile.go`
- Test: `server/internal/battle/projectile_test.go`

**Interfaces:**
- Produces:

```go
const (ProjStraight uint8=0; ProjLob=1)
type Projectile struct{ ID uint32; Kind, OwnerSlot uint8; X,Y,VX,VY float64
	Remain float64      // 剩余里程
	TargetX,TargetY float64 } // 抛射用
func (w *World) fire(slot int, in PlayerInput)      // 校验墨量/冷却/射程→创建弹体（服务端权威校验点）
func (w *World) stepProjectiles()                   // 单Tick推进全部弹体
```

规则（规格 2.3/四章逐条落地）：
- 预定里程 = `min(hypot(aim-pos), maxRange)`；直射沿途 `PaintCorridor`，终点 `SplatCircle(endSplatRadius)`；抛射仅终点 `SplatCircle(splatRadius)`
- 直射：黑洞加速度弯轨（速度方向变、速率不变），入 kill_radius 湮灭（Stats.BlackholeLost++）；撞普通墙提前落地（终点小滩涂在墙前）；撞反射墙按碰撞面法线镜像速度且 `Remain -= reflect_range_cost`（Stats.Reflects++）；命中敌方玩家（圆相交）→敌方 `SlowTicks=hit_slow_ticks`、原地小滩、弹销毁、Stats.Hits++
- 抛射：直线匀速飞向 Target，无碰撞；到达即炸；落点圈内敌人也吃减速
- 发射校验：墨量 ≥ cost、`Tick-LastFireTick[kind] >= fire_interval_ticks`，通过后扣墨、记冷却、Stats.XxxShots++

- [ ] **Step 1: 失败测试**（关键用例）

```go
func TestFireConsumesInkAndCooldown(t *testing.T) {
	w := newTestWorld(t)
	in := PlayerInput{FireStraight: true, AimX: 300, AimY: 360}
	w.Step([2]PlayerInput{in, {}})
	if len(w.Projectiles) != 1 || w.Players[0].Ink >= 100-w.Cfg.Ink.StraightCost+0.5 {
		t.Fatalf("proj=%d ink=%f", len(w.Projectiles), w.Players[0].Ink)
	}
	w.Step([2]PlayerInput{in, {}}) // 冷却未到(interval=4)
	if len(w.Projectiles) != 1 {
		t.Fatal("cooldown must block")
	}
}

func TestNoInkNoFire(t *testing.T) {
	w := newTestWorld(t)
	w.Players[0].Ink = 1
	w.Step([2]PlayerInput{{FireLob: true, AimX: 600, AimY: 360}, {}})
	if len(w.Projectiles) != 0 {
		t.Fatal("insufficient ink must block")
	}
}

func TestStraightLandsAtAimDistance(t *testing.T) {
	w := newTestWorld(t)
	w.Players[0].X, w.Players[0].Y = 200, 600 // 远离塔区/墙
	aim := PlayerInput{FireStraight: true, AimX: 350, AimY: 600} // 150 里程
	w.Step([2]PlayerInput{aim, {}})
	for i := 0; i < 60 && len(w.Projectiles) > 0; i++ {
		w.Step([2]PlayerInput{{}, {}})
	}
	if w.Grid.ColorAt(340, 600) != CellP0 { // 落点小滩
		t.Fatal("end splat missing")
	}
}

func TestLobIgnoresWallAndSplats(t *testing.T) {
	w := newTestWorld(t)
	w.Players[0].X, w.Players[0].Y = 350, 141 // 墙(400,130,130,22)左边
	w.Step([2]PlayerInput{{FireLob: true, AimX: 600, AimY: 141}, {}}) // 目标在墙后
	for i := 0; i < 90 && len(w.Projectiles) > 0; i++ {
		w.Step([2]PlayerInput{{}, {}})
	}
	if w.Grid.ColorAt(600, 141+30) != CellP0 { // 墙后炸开
		t.Fatal("lob must cross wall and splat")
	}
}

func TestBlackholeKillsStraight(t *testing.T) {
	w := newTestWorld(t)
	w.Players[0].X, w.Players[0].Y = 640-200, 360
	w.Step([2]PlayerInput{{FireStraight: true, AimX: 640, AimY: 360}, {}})
	for i := 0; i < 60 && len(w.Projectiles) > 0; i++ {
		w.Step([2]PlayerInput{{}, {}})
	}
	if w.Players[0].Stats.BlackholeLost != 1 {
		t.Fatal("must be annihilated")
	}
}

func TestReflectWallBounces(t *testing.T) {
	w := newTestWorld(t)
	w.Players[0].X, w.Players[0].Y = 605, 200 // 反射墙(560,80,90,16)正下方
	w.Step([2]PlayerInput{{FireStraight: true, AimX: 605, AimY: 60}, {}})
	for i := 0; i < 90 && len(w.Projectiles) > 0; i++ {
		w.Step([2]PlayerInput{{}, {}})
	}
	if w.Players[0].Stats.Reflects != 1 {
		t.Fatal("must reflect once")
	}
}

func TestHitEnemyAppliesSlow(t *testing.T) {
	w := newTestWorld(t)
	w.Players[0].X, w.Players[0].Y = 200, 600
	w.Players[1].X, w.Players[1].Y = 300, 600
	w.Step([2]PlayerInput{{FireStraight: true, AimX: 400, AimY: 600}, {}})
	for i := 0; i < 30 && len(w.Projectiles) > 0; i++ {
		w.Step([2]PlayerInput{{}, {}})
	}
	if w.Players[1].SlowTicks <= 0 || w.Players[0].Stats.Hits != 1 {
		t.Fatal("hit must slow enemy")
	}
}
```

- [ ] **Step 2: 实现要点**（直射单 Tick 推进）

```go
func (w *World) stepStraight(pr *Projectile, dt float64) (dead bool) {
	sp := w.Cfg.Straight.Speed
	// 黑洞弯轨：加速度→方向变、速率归一
	for _, bh := range w.Map.Blackholes {
		d := math.Hypot(pr.X-bh.X, pr.Y-bh.Y)
		if d < w.Cfg.Blackhole.KillRadius {
			w.Players[pr.OwnerSlot].Stats.BlackholeLost++
			return true
		}
		if d < w.Cfg.Blackhole.EffectRadius {
			g := w.Cfg.Blackhole.ProjGravity * (1 - d/w.Cfg.Blackhole.EffectRadius)
			pr.VX += (bh.X - pr.X) / d * g * dt
			pr.VY += (bh.Y - pr.Y) / d * g * dt
			n := math.Hypot(pr.VX, pr.VY)
			pr.VX, pr.VY = pr.VX/n*sp, pr.VY/n*sp
		}
	}
	stepLen := math.Min(sp*dt, pr.Remain)
	nx, ny := pr.X+pr.VX/sp*stepLen, pr.Y+pr.VY/sp*stepLen
	own := CellP0
	if pr.OwnerSlot == 1 { own = CellP1 }
	// 墙检测（点采样步进，步长=半径）
	if wall, hitX, hitY := w.segmentHitsWall(pr.X, pr.Y, nx, ny); wall != nil {
		if wall.Reflect {
			w.reflectVelocity(pr, wall)
			pr.Remain -= w.Cfg.ReflectRangeCost
			w.Players[pr.OwnerSlot].Stats.Reflects++
			if pr.Remain <= 0 {
				w.land(pr, hitX, hitY, own)
				return true
			}
			pr.X, pr.Y = hitX, hitY
			return false
		}
		PaintCorridor(w.Grid, pr.X, pr.Y, hitX, hitY, own, w.Cfg.Straight.TrailSideProb, w.rng)
		w.land(pr, hitX, hitY, own)
		return true
	}
	PaintCorridor(w.Grid, pr.X, pr.Y, nx, ny, own, w.Cfg.Straight.TrailSideProb, w.rng)
	pr.X, pr.Y, pr.Remain = nx, ny, pr.Remain-stepLen
	// 命中敌人
	enemy := w.Players[1-pr.OwnerSlot]
	if enemy.Online && math.Hypot(enemy.X-pr.X, enemy.Y-pr.Y) < w.Cfg.Player.Radius+w.Cfg.Straight.Radius {
		enemy.SlowTicks = w.Cfg.Straight.HitSlowTicks
		w.Players[pr.OwnerSlot].Stats.Hits++
		w.land(pr, pr.X, pr.Y, own)
		return true
	}
	if pr.Remain <= 0 {
		w.land(pr, pr.X, pr.Y, own)
		return true
	}
	return false
}
// land: SplatCircle(EndSplatRadius) + Stats.PaintedCells 累加
// reflectVelocity: 按命中面（矩形四边中最近者）取轴镜像 VX 或 VY
```

- [ ] **Step 3: 全绿提交** `feat(battle): 双弹道模拟——发射校验/黑洞/反射/命中/涂色`

---

### Task 6: 胜负——覆盖率/倒计时/时限/结算统计

**Files:**
- Create: `server/internal/battle/win.go`
- Test: `server/internal/battle/win_test.go`

**Interfaces:**
- Produces: `(*World).stepWin()`（Step 末尾调用）；填充 `w.Result`；导出 `(*World).CoverageNow() (c0,c1 float64)`

- [ ] **Step 1: 失败测试**

```go
func TestCountdownTriggerAndInterrupt(t *testing.T) {
	w := newTestWorld(t)
	fillCoverage(w, 0, 0.80) // 测试助手：把80%可涂格子涂成P0色
	w.Step([2]PlayerInput{{}, {}})
	if w.Countdown.Leader != 0 || w.Countdown.Ticks != 1 {
		t.Fatalf("cd=%+v", w.Countdown)
	}
	fillCoverage(w, 0, 0.50) // 压回75%以下
	w.Step([2]PlayerInput{{}, {}})
	if w.Countdown.Leader != 0xFF {
		t.Fatal("countdown must reset")
	}
}

func TestCountdownWin(t *testing.T) {
	w := newTestWorld(t)
	fillCoverage(w, 1, 0.80)
	for i := 0; i <= w.Cfg.WinCountdownTicks && w.Result == nil; i++ {
		w.Step([2]PlayerInput{{}, {}})
	}
	if w.Result == nil || w.Result.WinnerSlot != 1 {
		t.Fatalf("res=%+v", w.Result)
	}
}

func TestTimeLimitJudge(t *testing.T) {
	w := newTestWorld(t)
	fillCoverage(w, 0, 0.30) // 30% vs 塔区基础
	w.Tick = w.Cfg.MatchDurationTicks - 1
	w.Step([2]PlayerInput{{}, {}})
	if w.Result == nil || w.Result.WinnerSlot != 0 {
		t.Fatalf("res=%+v", w.Result)
	}
}
```

`fillCoverage` 助手：遍历 MaskFree 格子按比例 Paint。

- [ ] **Step 2: 实现**

```go
func (w *World) stepWin() {
	if w.Result != nil {
		return
	}
	c0, c1 := w.Grid.Coverage()
	lead, cov := uint8(0xFF), 0.0
	if c0 >= w.Cfg.WinRatio {
		lead, cov = 0, c0
	} else if c1 >= w.Cfg.WinRatio {
		lead, cov = 1, c1
	}
	_ = cov
	if lead == 0xFF {
		w.Countdown.Leader = 0xFF
		w.Countdown.Ticks = 0
	} else if w.Countdown.Leader == lead {
		w.Countdown.Ticks++
		if w.Countdown.Ticks >= w.Cfg.WinCountdownTicks {
			w.Result = &Result{WinnerSlot: lead, Cov: [2]float64{c0, c1}, DurationTicks: w.Tick}
			return
		}
	} else {
		w.Countdown.Leader = lead
		w.Countdown.Ticks = 1
	}
	if w.Tick >= w.Cfg.MatchDurationTicks {
		r := &Result{Cov: [2]float64{c0, c1}, DurationTicks: w.Tick}
		switch {
		case c0 > c1:
			r.WinnerSlot = 0
		case c1 > c0:
			r.WinnerSlot = 1
		default:
			r.WinnerSlot, r.Draw = 0xFF, true
		}
		w.Result = r
	}
}
```

- [ ] **Step 3: 全绿提交** `feat(battle): 胜负判定——75%倒计时可打断/时限对比/平局`

---

### Task 7: 战斗二进制编解码（codec.go）

**Files:**
- Create: `server/internal/battle/codec.go`
- Test: `server/internal/battle/codec_test.go`

**Interfaces:**
- Produces（所有布局大端）:

```go
// 输入包(客户端→服务端, MsgBattleInput) 7字节:
//   moveX int8 | moveY int8 | buttons uint8(bit0直射 bit1抛射) | aimX uint16 | aimY uint16
func EncodeInput(in PlayerInput) []byte
func DecodeInput(b []byte) (PlayerInput, error)

// 状态包(服务端→客户端, MsgBattleState):
//   tick uint32 | 玩家×2{ x f32 | y f32 | ink uint8 | flags uint8(bit0slow bit1online) }
//   | projCount uint8 ×{ id uint32 | kind uint8 | owner uint8 | x f32 | y f32 }
//   | cov0 uint16(万分比) | cov1 uint16 | cdLeader uint8 | cdTicks uint16 | leftTicks uint16
//   | dirtyCount uint16 ×{ packed uint16 }
func EncodeState(w *World, dirty []uint16) []byte
// 快照包(MsgBattleSnapshot): mySlot uint8 | tick uint32 | rleLen uint16 | rle...
func EncodeSnapshot(w *World, mySlot uint8) []byte
```

- [ ] **Step 1: 失败测试**（输入 roundtrip；状态包手工构造 World 后断言字节布局；快照含 Task 2 的 DecodeSnapshot 对拍）

```go
func TestInputRoundtrip(t *testing.T) {
	in := PlayerInput{MoveX: -50, MoveY: 100, FireLob: true, AimX: 640, AimY: 360}
	out, err := DecodeInput(EncodeInput(in))
	if err != nil || out != in {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestStatePacketLayout(t *testing.T) {
	w := newTestWorld(t)
	w.Step([2]PlayerInput{{}, {}})
	b := EncodeState(w, w.Grid.DrainDirty())
	if binary.BigEndian.Uint32(b[0:4]) != uint32(w.Tick) {
		t.Fatal("tick field")
	}
	// 玩家0 x
	x := math.Float32frombits(binary.BigEndian.Uint32(b[4:8]))
	if math.Abs(float64(x)-w.Players[0].X) > 0.01 {
		t.Fatalf("x=%f", x)
	}
}
```

- [ ] **Step 2: 实现**（逐字段 binary.BigEndian 写读，长度不足返回 error；无反射无库）
- [ ] **Step 3: 全绿提交** `feat(battle): 输入/状态/快照二进制编解码`

---

### Task 8: 消息 ID 追加 + Room goroutine

**Files:**
- Modify: `server/internal/protocol/msgid.go`
- Create: `server/internal/room/room.go`
- Test: `server/internal/room/room_test.go`

**Interfaces:**
- 追加 msgID：

```go
// 战斗 300-349
MsgRoomJoin=300; MsgRoomJoinOK=301; MsgBattleSnapshot=303; MsgBattleInput=310
MsgBattleState=320; MsgBattleSettle=340; MsgRoomErr=349
// 内部 400-449（Task 9）
MsgRoomdRegister=400; MsgRoomCreate=401; MsgRoomdLoad=402; MsgRoomResult=403
MsgFwd=410; MsgPush=411
```

- Produces:

```go
type Sink interface{ Push(uid int64, msgID uint16, body []byte) } // 房间→外界的唯一出口
type ResultFn func(res *battle.Result, w *battle.World, uids [2]int64)
func NewRoom(id int64, uids [2]int64, cfg *battle.Config, m *battle.MapDef,
	sink Sink, onResult ResultFn) *Room
func (r *Room) Start()                      // 起 goroutine（内部 ticker 30Hz）
func (r *Room) StartManual() / (r *Room) TickOnce()  // 测试用：手动步进（不起 ticker）
func (r *Room) Join(uid int64)              // 玩家就绪/重连：置 Online、发 JoinOK+Snapshot
func (r *Room) Disconnect(uid int64)        // 断线：置 Offline 开始计 OfflineTicks
func (r *Room) Input(uid int64, raw []byte) // 解码入队；限流(fire_rate 令牌桶)超限丢弃
func (r *Room) Done() <-chan struct{}
```

房间内规则：
- 双人都 Join 过一次才开始 Step（等待期广播不动）；输入队列 chan 容量 64，每 Tick 每玩家取最新一条（旧输入覆盖式——最后到达者生效）
- 每 Tick：Step → `EncodeState` → Sink.Push×2（离线者跳过）
- **占比时间序列**：每 30 Tick（1 秒）把双方当前占比追加到 `covHistory [][2]float32`（最多 181 点），结算时随 MsgBattleSettle JSON 下发——结算面板占比曲线的数据源（规格 2.7）
- 任一玩家 `OfflineTicks > reconnect_grace_ticks` → 对方直接胜（构造 Result）
- `w.Result != nil` → 广播 `MsgBattleSettle`（JSON：胜者 UID/占比/covHistory/双方 Stats/时长）→ 调 onResult → settle_destroy_ticks 后关闭 goroutine（close Done）

- [ ] **Step 1: 失败测试**（fakeSink 收集推送；TickOnce 驱动，不依赖真实时间）

```go
type fakeSink struct{ msgs []struct{ UID int64; MsgID uint16; Body []byte } }
func (f *fakeSink) Push(uid int64, id uint16, b []byte) {
	f.msgs = append(f.msgs, struct{ UID int64; MsgID uint16; Body []byte }{uid, id, b})
}

func TestJoinSendsSnapshotAndStatesFlow(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	sink := &fakeSink{}
	r := NewRoom(1, [2]int64{10, 20}, cfg, m, sink, func(*battle.Result, *battle.World, [2]int64) {})
	r.StartManual()
	r.Join(10)
	r.Join(20)
	// Join 后各收到 JoinOK + Snapshot
	if countMsg(sink, protocol.MsgBattleSnapshot) != 2 {
		t.Fatal("both must get snapshot")
	}
	r.TickOnce()
	if countMsg(sink, protocol.MsgBattleState) != 2 {
		t.Fatal("state broadcast to both")
	}
}

func TestOfflineTimeoutWins(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	sink := &fakeSink{}
	var got *battle.Result
	r := NewRoom(1, [2]int64{10, 20}, cfg, m, sink,
		func(res *battle.Result, w *battle.World, uids [2]int64) { got = res })
	r.StartManual()
	r.Join(10); r.Join(20)
	r.Disconnect(20)
	for i := 0; i <= cfg.ReconnectGraceTicks+1 && got == nil; i++ {
		r.TickOnce()
	}
	if got == nil || got.WinnerSlot != 0 {
		t.Fatalf("res=%+v", got)
	}
}

func TestInputAppliedAndRateLimited(t *testing.T) {
	cfg, m, _ := battle.LoadConfig("../../configs/battle.json", "../../configs/map_01.json")
	sink := &fakeSink{}
	r := NewRoom(1, [2]int64{10, 20}, cfg, m, sink, func(*battle.Result, *battle.World, [2]int64) {})
	r.StartManual()
	r.Join(10); r.Join(20)
	r.Input(10, battle.EncodeInput(battle.PlayerInput{MoveX: 100}))
	x0 := r.world.Players[0].X
	r.TickOnce()
	if r.world.Players[0].X <= x0 {
		t.Fatal("input must move player")
	}
}
```

- [ ] **Step 2: 实现**（覆盖式输入槽 `latest [2]atomicInput`+互斥即可；发射限流：`gateway.NewTokenBucket(cfg.FireRate...)` 仅对 buttons!=0 的输入计数，超限清零 fire 位）
- [ ] **Step 3: 全绿提交** `feat(room): 房间goroutine——输入/广播/断线判负/结算回调`

---

### Task 9: roomd 进程与网关转发

**Files:**
- Create: `server/internal/room/manager.go`、`server/cmd/roomd/main.go`、`server/internal/gateway/forward.go`
- Modify: `server/internal/gateway/server.go`（内部监听端口）、`server/cmd/gateway/main.go`、`server/configs/server.json`（加 `internal_addr:"0.0.0.0:8090"`）、`server/configs/roomd.json`（`gateway_addr`、`max_rooms`）
- Test: `server/internal/gateway/forward_test.go`

**协议（全部走既有 Frame，body 布局）：**
- `MsgRoomdRegister`(roomd→gw)：`maxRooms uint16`
- `MsgRoomdLoad`(roomd→gw，每 5s)：`activeRooms uint16`
- `MsgRoomCreate`(gw→roomd)：`roomID int64 | uidA int64 | uidB int64`
- `MsgRoomResult`(roomd→gw)：JSON `{room_id, winner_uid, loser_uid, draw, stats_a:{...}, stats_b:{...}, duration}`（入大厅 EventQueue）
- `MsgFwd`(gw→roomd)：`sessionUID int64 | origMsgID uint16 | origBody...`（客户端 300-349 消息包装转发；网关以 **UID** 而非 sessionID 标识——重连后新会话仍路由正确）
- `MsgPush`(roomd→gw)：`uid int64 | msgID uint16 | body...`（网关查 Presence 推给该 UID 当前会话）

**Interfaces:**
- `gateway.NewRoomdRegistry() *RoomdRegistry`：`Register(conn *RoomdConn, maxRooms int)`、`ReportLoad(conn, active int)`、`PickLeastLoaded() (*RoomdConn, error)`（无可用返回 error；测试覆盖：两实例负载不同选低者、满载全拒）
- `gateway.ForwardTable`：`BindUID(uid int64, rc *RoomdConn)`、`Lookup(uid) (*RoomdConn, bool)`、`Unbind(uid)`
- 网关侧：Router 上把 300-349 全注册为 forward handler（未绑定 roomd 回 MsgRoomErr）；lobby `SetRoomAllocator` 改为：Pick→发 MsgRoomCreate→BindUID×2→返回 roomID（roomID 用大厅自增）
- roomd 侧：`room.Manager` 持有 rooms map，收 MsgRoomCreate 建房；收 MsgFwd 解出 uid/origMsgID → Join/Input/Disconnect；Sink 实现为写回 MsgPush；对局结束发 MsgRoomResult 并删房

- [ ] **Step 1: RoomdRegistry/ForwardTable 单测先行**（纯内存逻辑，同 Task 5 限流器测试风格：两连接注册→ReportLoad(1,5)→Pick 返回低负载者；满载返回 error）
- [ ] **Step 2: 实现 registry/forward + 网关内部监听**（`internal_addr` 独立 accept 循环，仅接受 roomd 帧：Register/Load/Push/Result 四类 handler）
- [ ] **Step 3: roomd main**：读 roomd.json → dial gateway internal_addr → 发 Register → 读循环处理 Create/Fwd → ticker 5s 报负载；SIGTERM 优雅：停接新房（Register maxRooms=0 重报），存量房结束后退出（规格 6.1 排空缩容）；**配置热更**：收到 MsgRoomCreate 建房时，若 battle.json/map_01.json 的 mtime 变化则重新加载——新对局用新配置、局内快照不变、进程不重启（规格八"热更不重启"），附单测：改 mtime 后建房拿到新值
- [ ] **Step 4: 联调冒烟**：起 MySQL+gateway+roomd → 两个 Go 临时客户端（或计划 3 客户端）完成匹配 → 网关日志出现 RoomCreate → 客户端发 MsgRoomJoin(JSON `{room_id, token}`，网关验 token 后包装转发) → 收到 Snapshot 与持续 State 包
- [ ] **Step 5: 全绿提交** `feat(roomd): 房间进程/注册中心/负载分配/UID转发/结果回传`

---

### Task 10: Unity——BattleCodec 与战斗场景网络接入

**Files:**
- Create: `Assets/Scripts/Game/BattleCodec.cs`、改造 `Assets/Scripts/Game/BattleController.cs`（替换 Stub）
- Test: `Assets/Tests/EditMode/BattleCodecTests.cs`

**Interfaces:**
- `BattleCodec.EncodeInput(sbyte moveX, sbyte moveY, byte buttons, ushort aimX, ushort aimY) : byte[]`（7 字节，与 Go DecodeInput 对拍）
- `BattleCodec.DecodeState(byte[]) : StateMsg`（结构体镜像 Task 7 布局：players[2]{x,y,ink,flags}、projs[]、cov0/cov1、cdLeader/cdTicks/leftTicks、dirty[]）
- `BattleCodec.DecodeSnapshot(byte[]) : (byte mySlot, uint tick, byte[] colors9216)`（C# 版 RLE 解码）
- `BattleController`：Start 时 `Send(MsgRoomJoin, {room_id, token})`；`InvokeRepeating(SendInput, 0f, 1f/30f)` 采集 WASD+鼠标+左右键；收 State 入环形缓冲
- **断线自动重连编排**（规格 2.7/6.1 客户端侧）：订阅 `NetworkClient.OnDisconnected` → 显示"重连中…"遮罩 → 每 2 秒重试 `Connect`（最多 15 次≈30 秒重连窗口）→ 成功后先 `MsgLoginReq`（复用已存凭据/ReconnectToken）→ 再 `MsgRoomJoin{room_id, token}` → 收到新 Snapshot 覆盖战场、隐藏遮罩；超过窗口仍失败则回登录场景

- [ ] **Step 1: EditMode 失败测试**（EncodeInput 字节断言 ↔ 手写 Go 输出样例；RLE 解码用 Go `Snapshot()` 真实输出的十六进制字面量对拍）
- [ ] **Step 2: 实现两个 codec + Controller 网络骨架 + 断线自动重连编排**
- [ ] **Step 3: 测试全绿提交** `feat(battle-client): C#战斗编解码/入房/输入流/断线自动重连`

---

### Task 11: Unity——涂色渲染与实体插值

**Files:**
- Create: `Assets/Scripts/Game/PaintRenderer.cs`、`Assets/Scripts/Game/EntityView.cs`；Battle.unity 场景搭建

**实现决策（全部定死）：**
- 相机：正交 size=360，位于 (640,360)；背景 #06080F
- 涂色层：`Texture2D(128,72, RGBA32)`，`filterMode=Point`，贴在 1280×720 的 SpriteRenderer/Quad 上；颜色：P0=#22D3EE(55% alpha)、P1=#F472B6(55% alpha)、None=透明；快照到达时全量 SetPixels32，每个 State 包只对 dirty 格 SetPixel，帧末 `Apply(false)`
- 静态层：按 map_01.json 在场景中摆墙(#334155)、反射墙(#FBBF24 描边)、黑洞(黑圆+紫环)、双塔(青/品红圆环)——Editor 手工摆放，坐标照配置
- 实体：玩家=三角 Sprite（Unity 内置 Knob/Triangle 或代码生成），弹体=小圆点+TrailRenderer；**插值**：State 缓冲 ≥2 帧，渲染时间 = 最新服务器 Tick 时间 −100ms，两帧间 Lerp；弹体按 id 建/销 GameObject
- 抛射弹落点预警圈：kind==1 的弹体在其 Target 处画虚线圈——State 包无 Target 字段 → **修正**：Task 7 状态包弹体段追加 `targetX f32 | targetY f32`（仅 lob 有意义，直射填当前位置），C# 侧同步

- [ ] **Step 1: 实现 PaintRenderer（快照全量+增量）**
- [ ] **Step 2: 实现 EntityView 插值**
- [ ] **Step 3: 双端联调冒烟**：两客户端对战，验证——移动顺滑无跳变、左键墨线沿途出现、右键越墙炸开、黑洞弯轨可见、双方画面涂色一致
- [ ] **Step 4: 提交** `feat(battle-client): 涂色贴图渲染与实体插值`

---

### Task 12: Unity——HUD 与结算面板

**Files:**
- Create: `Assets/Scripts/UI/BattleHud.cs`、`Assets/Scripts/UI/SettlePanel.cs`；Battle.unity 追加 UI

**HUD 元素（数据来自 State/Settle 包 + BattleContext 昵称）：**
- 顶部两侧：双方昵称（左 `BattleContext.MyNickname` 青色 / 右 `BattleContext.OpponentNickname` 品红）
- 顶部中央：双色占比条（两个 Image fillAmount= cov/10000）+ 数字百分比 + 剩余时间 mm:ss（leftTicks/30）
- 左下：能量条（自己 slot 的 ink/100，UI 文案叫"能量"= 信号能量），不足弹耗时闪红
- 中央大字：倒计时（cdLeader!=0xFF 时显示"⚠ 对方即将获胜 N"或"胜利倒计时 N"，N=ceil(cdTicks 剩余/30)）
- 被命中：全屏边缘泛红 0.3s（flags bit0）
- **终局演出**（规格 2.7）：收到 Settle 后先播胜方颜色以其主塔为中心涌满全屏的动画（0.8s，用一个覆盖全屏的 Image 从塔位置放大 + 染胜方色），平局则双色从两塔对涌各占半屏，演出结束再淡入 SettlePanel
- SettlePanel：收 MsgBattleSettle(JSON) 后弹出——胜方昵称+胜负大字（如"XXX 胜利"，根据 winner UID 映射 BattleContext 昵称）/最终占比/**占比变化曲线**（用 covHistory 逐点连线绘制到一张 RawImage，青/品红两条折线）/我的统计（信号占领格数、双弹发射数、命中、反射、湮灭）/**ELO 变化**（收 MsgEloUpdate 推送填入，未到则显示"结算中…"）→【返回大厅】按钮 LoadScene("Lobby")
- ELO 推送处理：注册 `MsgId.EloUpdate` handler，解析 `{old_elo,new_elo,delta}` 更新面板

- [ ] **Step 1: 搭 UI + 实现两脚本**
- [ ] **Step 2: 完整对局冒烟**：打满一局 3 分钟（或改配置 30s 短局）验证时限判定；再验证 75% 倒计时触发与打断显示
- [ ] **Step 3: 提交** `feat(battle-client): HUD占比/墨量/倒计时与结算面板`

---

### Task 13: 端到端验收（E2E）

- [ ] **Step 1: 全链路清单验证**（MySQL + gateway + roomd ×2 + 两客户端）
  1. 双端注册登录 → 匹配 → 同房间开战（观察两 roomd 谁接单：先起者负载低）
  2. 对战 3 分钟涂色胜负 → 双端结算面板一致 → 返回大厅 ELO 已变
  3. 一端强杀进程 → 30 秒内重启客户端登录 → 凭 Token `MsgRoomJoin` 回房 → 快照恢复战场 → 打完
  4. 一端强杀不回 → 30 秒后另一端直接胜利结算
  5. `game_match_history` 两行记录字段完整；网关/大厅/roomd 日志无 ERROR
- [ ] **Step 2: `go test ./... -race` 全绿 + `go vet` 干净**
- [ ] **Step 3: 双仓库提交** `test(e2e): 全链路对局/重连/判负验收通过`

---

## Self-Review 结果

1. **规格覆盖**：两层坐标系(T2,T4)、溅射浓度(T3)、双弹道全规则(T5)、墨量踩色(T4)、胜负倒计时可打断(T6)、指令流上行/实体全量+脏格增量/RLE 快照(T7,T8)、防作弊校验（移速上限 T4、墨量/冷却 T5、发射限流 T8、服务端权威全程）、断线重连 Token+快照+超时判负(T8,T9,T13)、房间集群注册/负载/排空(T9)、结算异步入库(T9→计划2 EventQueue)、终局演出与结算面板(T12)。
2. **修正记录**：BlackholeDef 增加 `nopaint_radius`(T2)；状态包弹体段增加 target 坐标(T11 反馈到 T7 布局，实现 T7 时直接带上)。
3. **类型一致性**：`battle.PlayerInput/Result` 贯穿 room/roomd；转发以 UID 为键（重连安全）；C# StateMsg 字段与 Go 布局逐字节对齐（T10 用 Go 真实输出对拍）。
4. **范围外**（转计划 5）：Bot、观战、docker-compose、监控指标。
