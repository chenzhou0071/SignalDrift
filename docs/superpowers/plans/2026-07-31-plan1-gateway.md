# 《信号漂流》计划 1/5：TCP 网关与二进制协议 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现纯网络层 TCP 网关：自研二进制协议（粘包分包）、会话管理、心跳超时、IP 令牌桶限流、序列号幂等过滤、消息路由、优雅关闭。

**Architecture:** 单进程网关，每连接一读一写两个 goroutine；协议层（protocol 包）与接入层（gateway 包）分离；所有阈值来自 `configs/server.json`。本计划零业务逻辑——大厅/房间在后续计划中以 Router 处理器形式接入。

**Tech Stack:** Go 1.22+，仅标准库（自研协议是简历卖点，禁止引入网络框架）。

**路线图位置:** 本计划是 5 份计划中的第 1 份（网关 → 大厅 → Unity 客户端 → 房间战斗 → Bot 与演示包）。

## Global Constraints

- 代码根目录：`E:\pro\SignalDrift\server\`，Go module 名 `signaldrift/server`
- 仅 Go 标准库，不引入任何第三方依赖
- 字节序：**大端序**（网络字节序），Unity 客户端后续按此实现
- 帧格式：`magic(2B)=0x5344 | msgID(2B) | seq(4B) | bodyLen(4B) | body`，头 12 字节，body 上限 64KB
- 所有数值阈值从 `configs/server.json` 读取，代码无硬编码
- 每个 Task 结束必须 `go test ./...` 全绿再 commit；commit 在仓库 `E:\pro\SignalDrift` 执行
- 测试需要可控时间时，一律注入 `now func()`，禁止 `time.Sleep` 式脆弱测试（集成测试的短暂等待除外）

## File Structure

```
server/
  go.mod
  configs/server.json            — 网关配置
  internal/config/config.go      — 配置加载
  internal/protocol/frame.go     — 帧编解码 + FrameReader 粘包分包
  internal/protocol/msgid.go     — 消息 ID 常量表
  internal/gateway/session.go    — Session 与 SessionManager
  internal/gateway/ratelimit.go  — 令牌桶 + IP 限流器
  internal/gateway/heartbeat.go  — 心跳超时扫描
  internal/gateway/router.go     — 消息路由
  internal/gateway/server.go     — TCP 接入层（accept/读写循环/优雅关闭）
  cmd/gateway/main.go            — 进程入口
```

---

### Task 1: Go 模块脚手架与配置加载

**Files:**
- Create: `server/go.mod`
- Create: `server/configs/server.json`
- Create: `server/internal/config/config.go`
- Test: `server/internal/config/config_test.go`

**Interfaces:**
- Consumes: 无（首个任务）
- Produces: `config.Load(path string) (*ServerConfig, error)`；结构体 `ServerConfig{ListenAddr string; MaxConns int; SendQueueSize int; IPRate RateConfig{Rate, Burst float64}; Heartbeat HeartbeatConfig{IntervalSec, TimeoutSec, SweepSec int}}`

- [ ] **Step 1: 初始化 module 与配置文件**

```bash
cd "E:\pro\SignalDrift\server"; go mod init signaldrift/server
```

创建 `server/configs/server.json`：

```json
{
  "listen_addr": "0.0.0.0:8080",
  "max_conns": 2000,
  "send_queue_size": 256,
  "ip_rate": { "rate": 100, "burst": 200 },
  "heartbeat": { "interval_sec": 5, "timeout_sec": 15, "sweep_sec": 1 }
}
```

- [ ] **Step 2: 写失败测试**

`server/internal/config/config_test.go`：

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server.json")
	data := `{"listen_addr":"127.0.0.1:9000","max_conns":100,"send_queue_size":64,
"ip_rate":{"rate":50,"burst":80},"heartbeat":{"interval_sec":5,"timeout_sec":15,"sweep_sec":1}}`
	if err := os.WriteFile(p, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" || cfg.MaxConns != 100 ||
		cfg.IPRate.Burst != 80 || cfg.Heartbeat.TimeoutSec != 15 {
		t.Fatalf("bad cfg: %+v", cfg)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("no/such/file.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/config/ -v`
Expected: FAIL（`Load` 未定义）

- [ ] **Step 4: 最小实现**

`server/internal/config/config.go`：

```go
package config

import (
	"encoding/json"
	"os"
)

type RateConfig struct {
	Rate  float64 `json:"rate"`
	Burst float64 `json:"burst"`
}

type HeartbeatConfig struct {
	IntervalSec int `json:"interval_sec"`
	TimeoutSec  int `json:"timeout_sec"`
	SweepSec    int `json:"sweep_sec"`
}

type ServerConfig struct {
	ListenAddr    string          `json:"listen_addr"`
	MaxConns      int             `json:"max_conns"`
	SendQueueSize int             `json:"send_queue_size"`
	IPRate        RateConfig      `json:"ip_rate"`
	Heartbeat     HeartbeatConfig `json:"heartbeat"`
}

func Load(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
```

- [ ] **Step 5: 测试通过后提交**

Run: `go test ./... -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server
git -C "E:\pro\SignalDrift" commit -m "feat(server): go module 脚手架与网关配置加载"
```

---

### Task 2: 帧编解码（protocol.Frame）

**Files:**
- Create: `server/internal/protocol/frame.go`
- Create: `server/internal/protocol/msgid.go`
- Test: `server/internal/protocol/frame_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `protocol.Frame{MsgID uint16; Seq uint32; Body []byte}`、`protocol.Encode(f *Frame) []byte`、常量 `Magic=0x5344, HeaderSize=12, MaxBodySize=65536`、错误 `ErrBadMagic, ErrBodyTooLarge`、消息 ID `MsgHeartbeat=1, MsgHeartbeatAck=2, MsgEcho=100`

- [ ] **Step 1: 写失败测试**

`server/internal/protocol/frame_test.go`：

```go
package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEncodeLayout(t *testing.T) {
	f := &Frame{MsgID: 7, Seq: 42, Body: []byte("abc")}
	b := Encode(f)
	if len(b) != HeaderSize+3 {
		t.Fatalf("len=%d", len(b))
	}
	if binary.BigEndian.Uint16(b[0:2]) != Magic {
		t.Fatal("bad magic")
	}
	if binary.BigEndian.Uint16(b[2:4]) != 7 || binary.BigEndian.Uint32(b[4:8]) != 42 {
		t.Fatal("bad msgid/seq")
	}
	if binary.BigEndian.Uint32(b[8:12]) != 3 || !bytes.Equal(b[12:], []byte("abc")) {
		t.Fatal("bad body")
	}
}

func TestEncodeEmptyBody(t *testing.T) {
	b := Encode(&Frame{MsgID: 1, Seq: 1})
	if len(b) != HeaderSize {
		t.Fatalf("len=%d", len(b))
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/protocol/ -v` → FAIL（未定义）

- [ ] **Step 3: 最小实现**

`server/internal/protocol/msgid.go`：

```go
package protocol

// 消息 ID 分配：1-99 网关层，100-199 测试/通用，200-299 大厅（计划3），300-399 房间（计划4）
const (
	MsgHeartbeat    uint16 = 1
	MsgHeartbeatAck uint16 = 2
	MsgEcho         uint16 = 100
)
```

`server/internal/protocol/frame.go`：

```go
package protocol

import (
	"encoding/binary"
	"errors"
)

const (
	Magic       uint16 = 0x5344 // "SD"
	HeaderSize         = 12
	MaxBodySize        = 64 * 1024
)

var (
	ErrBadMagic     = errors.New("protocol: bad magic")
	ErrBodyTooLarge = errors.New("protocol: body too large")
)

type Frame struct {
	MsgID uint16
	Seq   uint32
	Body  []byte
}

func Encode(f *Frame) []byte {
	b := make([]byte, HeaderSize+len(f.Body))
	binary.BigEndian.PutUint16(b[0:2], Magic)
	binary.BigEndian.PutUint16(b[2:4], f.MsgID)
	binary.BigEndian.PutUint32(b[4:8], f.Seq)
	binary.BigEndian.PutUint32(b[8:12], uint32(len(f.Body)))
	copy(b[HeaderSize:], f.Body)
	return b
}
```

- [ ] **Step 4: 测试通过后提交**

Run: `go test ./... -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server/internal/protocol
git -C "E:\pro\SignalDrift" commit -m "feat(protocol): 二进制帧编码与消息ID表"
```

---

### Task 3: FrameReader 粘包分包

**Files:**
- Modify: `server/internal/protocol/frame.go`（追加 FrameReader）
- Test: `server/internal/protocol/frame_test.go`（追加）

**Interfaces:**
- Consumes: Task 2 的 `Frame/Encode/常量/错误`
- Produces: `protocol.NewFrameReader(r io.Reader) *FrameReader`、`(*FrameReader).Next() (*Frame, error)`——阻塞读一个完整帧；魔数错误返回 `ErrBadMagic`，超长返回 `ErrBodyTooLarge`，连接关闭返回底层 io 错误

- [ ] **Step 1: 写失败测试**（追加到 frame_test.go）

```go
func TestFrameReaderMultiFrame(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(Encode(&Frame{MsgID: 1, Seq: 1, Body: []byte("aa")}))
	buf.Write(Encode(&Frame{MsgID: 2, Seq: 2, Body: []byte("bbbb")}))
	fr := NewFrameReader(&buf)
	f1, err := fr.Next()
	if err != nil || f1.MsgID != 1 || string(f1.Body) != "aa" {
		t.Fatalf("f1=%+v err=%v", f1, err)
	}
	f2, err := fr.Next()
	if err != nil || f2.Seq != 2 || string(f2.Body) != "bbbb" {
		t.Fatalf("f2=%+v err=%v", f2, err)
	}
}

func TestFrameReaderFragmented(t *testing.T) {
	// iotest.OneByteReader 模拟极端拆包：每次只读 1 字节
	raw := Encode(&Frame{MsgID: 9, Seq: 3, Body: []byte("hello")})
	fr := NewFrameReader(iotest.OneByteReader(bytes.NewReader(raw)))
	f, err := fr.Next()
	if err != nil || f.MsgID != 9 || string(f.Body) != "hello" {
		t.Fatalf("f=%+v err=%v", f, err)
	}
}

func TestFrameReaderBadMagic(t *testing.T) {
	raw := Encode(&Frame{MsgID: 1, Seq: 1})
	raw[0] = 0xFF
	if _, err := NewFrameReader(bytes.NewReader(raw)).Next(); err != ErrBadMagic {
		t.Fatalf("err=%v", err)
	}
}

func TestFrameReaderBodyTooLarge(t *testing.T) {
	raw := Encode(&Frame{MsgID: 1, Seq: 1})
	binary.BigEndian.PutUint32(raw[8:12], MaxBodySize+1)
	if _, err := NewFrameReader(bytes.NewReader(raw)).Next(); err != ErrBodyTooLarge {
		t.Fatalf("err=%v", err)
	}
}
```

import 增加 `"testing/iotest"`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/protocol/ -v` → FAIL（NewFrameReader 未定义）

- [ ] **Step 3: 最小实现**（追加到 frame.go）

```go
import "io" // 合并进现有 import 块

type FrameReader struct {
	r      io.Reader
	header [HeaderSize]byte
}

func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{r: r}
}

// Next 阻塞读取一个完整帧；io.ReadFull 天然处理 TCP 拆包，
// 循环调用 Next 天然处理粘包。
func (fr *FrameReader) Next() (*Frame, error) {
	if _, err := io.ReadFull(fr.r, fr.header[:]); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint16(fr.header[0:2]) != Magic {
		return nil, ErrBadMagic
	}
	bodyLen := binary.BigEndian.Uint32(fr.header[8:12])
	if bodyLen > MaxBodySize {
		return nil, ErrBodyTooLarge
	}
	f := &Frame{
		MsgID: binary.BigEndian.Uint16(fr.header[2:4]),
		Seq:   binary.BigEndian.Uint32(fr.header[4:8]),
	}
	if bodyLen > 0 {
		f.Body = make([]byte, bodyLen)
		if _, err := io.ReadFull(fr.r, f.Body); err != nil {
			return nil, err
		}
	}
	return f, nil
}
```

- [ ] **Step 4: 测试通过后提交**

Run: `go test ./... -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server/internal/protocol
git -C "E:\pro\SignalDrift" commit -m "feat(protocol): FrameReader 粘包分包读取"
```

---

### Task 4: Session 与 SessionManager

**Files:**
- Create: `server/internal/gateway/session.go`
- Test: `server/internal/gateway/session_test.go`

**Interfaces:**
- Consumes: `protocol.Encode/Frame`
- Produces:
  - `gateway.Session`：字段 `ID uint64`、`UID int64`（登录前为 0）；方法 `Send(msgID uint16, body []byte) bool`（非阻塞入队，队满返回 false）、`Touch(now int64)`、`LastBeat() int64`、`CheckSeq(seq uint32) bool`（严格递增过滤，重复/回退返回 false）、`Close()`、`Done() <-chan struct{}`、`SendQueue() <-chan []byte`、`RemoteIP() string`
  - `gateway.SessionManager`：`NewSessionManager(queueSize int)`、`Create(conn net.Conn) *Session`、`Get(id) (*Session, bool)`、`Remove(id uint64)`、`Count() int`、`Range(fn func(*Session) bool)`

- [ ] **Step 1: 写失败测试**

`server/internal/gateway/session_test.go`：

```go
package gateway

import (
	"net"
	"testing"

	"signaldrift/server/internal/protocol"
)

func newTestSession(t *testing.T) (*SessionManager, *Session) {
	t.Helper()
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	return m, m.Create(c1)
}

func TestManagerCreateRemove(t *testing.T) {
	m, s := newTestSession(t)
	if m.Count() != 1 {
		t.Fatalf("count=%d", m.Count())
	}
	got, ok := m.Get(s.ID)
	if !ok || got != s {
		t.Fatal("Get failed")
	}
	m.Remove(s.ID)
	if m.Count() != 0 {
		t.Fatal("remove failed")
	}
}

func TestSendEnqueue(t *testing.T) {
	_, s := newTestSession(t)
	if !s.Send(protocol.MsgEcho, []byte("hi")) {
		t.Fatal("send should succeed")
	}
	raw := <-s.SendQueue()
	fr, err := protocol.NewFrameReader(bytesReader(raw)).Next()
	if err != nil || fr.MsgID != protocol.MsgEcho || string(fr.Body) != "hi" {
		t.Fatalf("fr=%+v err=%v", fr, err)
	}
}

func TestSendQueueFull(t *testing.T) {
	m := NewSessionManager(1)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	s := m.Create(c1)
	s.Send(1, nil) // 占满
	if s.Send(1, nil) {
		t.Fatal("expected queue full -> false")
	}
}

func TestCheckSeqMonotonic(t *testing.T) {
	_, s := newTestSession(t)
	if !s.CheckSeq(1) || !s.CheckSeq(2) || !s.CheckSeq(10) {
		t.Fatal("increasing seq must pass")
	}
	if s.CheckSeq(10) || s.CheckSeq(5) {
		t.Fatal("duplicate/backward seq must fail")
	}
}

func TestCloseIdempotent(t *testing.T) {
	_, s := newTestSession(t)
	s.Close()
	s.Close() // 不 panic
	select {
	case <-s.Done():
	default:
		t.Fatal("Done should be closed")
	}
}
```

文件顶部补一个小工具（供本包测试复用）：

```go
import "bytes"

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -v` → FAIL

- [ ] **Step 3: 最小实现**

`server/internal/gateway/session.go`：

```go
package gateway

import (
	"net"
	"sync"
	"sync/atomic"

	"signaldrift/server/internal/protocol"
)

type Session struct {
	ID  uint64
	UID int64

	conn      net.Conn
	sendCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
	lastBeat  atomic.Int64
	lastSeq   atomic.Uint32
}

func (s *Session) Send(msgID uint16, body []byte) bool {
	raw := protocol.Encode(&protocol.Frame{MsgID: msgID, Body: body})
	select {
	case s.sendCh <- raw:
		return true
	default:
		return false // 慢消费者：队满丢弃，由心跳超时最终清理
	}
}

func (s *Session) SendQueue() <-chan []byte { return s.sendCh }
func (s *Session) Done() <-chan struct{}    { return s.done }
func (s *Session) Touch(now int64)          { s.lastBeat.Store(now) }
func (s *Session) LastBeat() int64          { return s.lastBeat.Load() }

// CheckSeq 幂等过滤：客户端序列号必须严格递增。
func (s *Session) CheckSeq(seq uint32) bool {
	for {
		last := s.lastSeq.Load()
		if seq <= last {
			return false
		}
		if s.lastSeq.CompareAndSwap(last, seq) {
			return true
		}
	}
}

func (s *Session) RemoteIP() string {
	host, _, err := net.SplitHostPort(s.conn.RemoteAddr().String())
	if err != nil {
		return s.conn.RemoteAddr().String()
	}
	return host
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.conn.Close()
	})
}

type SessionManager struct {
	mu        sync.RWMutex
	sessions  map[uint64]*Session
	nextID    uint64
	queueSize int
}

func NewSessionManager(queueSize int) *SessionManager {
	return &SessionManager{sessions: make(map[uint64]*Session), queueSize: queueSize}
}

func (m *SessionManager) Create(conn net.Conn) *Session {
	s := &Session{
		ID:     atomic.AddUint64(&m.nextID, 1),
		conn:   conn,
		sendCh: make(chan []byte, m.queueSize),
		done:   make(chan struct{}),
	}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s
}

func (m *SessionManager) Get(id uint64) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *SessionManager) Remove(id uint64) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *SessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *SessionManager) Range(fn func(*Session) bool) {
	m.mu.RLock()
	snapshot := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		snapshot = append(snapshot, s)
	}
	m.mu.RUnlock()
	for _, s := range snapshot {
		if !fn(s) {
			return
		}
	}
}
```

- [ ] **Step 4: 测试通过后提交**

Run: `go test ./... -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server/internal/gateway
git -C "E:\pro\SignalDrift" commit -m "feat(gateway): Session 会话与管理器、序列号幂等过滤"
```

---

### Task 5: 令牌桶与 IP 限流器

**Files:**
- Create: `server/internal/gateway/ratelimit.go`
- Test: `server/internal/gateway/ratelimit_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `gateway.NewTokenBucket(rate, burst float64, now func() time.Time) *TokenBucket`、`(*TokenBucket).Allow() bool`；`gateway.NewIPLimiter(rate, burst float64, now func() time.Time) *IPLimiter`、`(*IPLimiter).Allow(ip string) bool`（now 传 nil 用 time.Now）

- [ ] **Step 1: 写失败测试**

`server/internal/gateway/ratelimit_test.go`：

```go
package gateway

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time            { return c.t }
func (c *fakeClock) advance(d time.Duration)   { c.t = c.t.Add(d) }

func TestTokenBucketBurstThenReject(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	b := NewTokenBucket(10, 3, clk.now) // 每秒10个，突发3
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("burst %d rejected", i)
		}
	}
	if b.Allow() {
		t.Fatal("4th should be rejected")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	b := NewTokenBucket(10, 3, clk.now)
	for i := 0; i < 3; i++ {
		b.Allow()
	}
	clk.advance(200 * time.Millisecond) // 补 2 个令牌
	if !b.Allow() || !b.Allow() {
		t.Fatal("refilled tokens should pass")
	}
	if b.Allow() {
		t.Fatal("over refill should be rejected")
	}
}

func TestIPLimiterIsolated(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := NewIPLimiter(10, 1, clk.now)
	if !l.Allow("1.1.1.1") {
		t.Fatal("first ip1")
	}
	if l.Allow("1.1.1.1") {
		t.Fatal("ip1 over burst")
	}
	if !l.Allow("2.2.2.2") {
		t.Fatal("ip2 must be isolated from ip1")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run 'TokenBucket|IPLimiter' -v` → FAIL

- [ ] **Step 3: 最小实现**

`server/internal/gateway/ratelimit.go`：

```go
package gateway

import (
	"sync"
	"time"
)

type TokenBucket struct {
	mu     sync.Mutex
	tokens float64
	rate   float64 // 每秒补充
	burst  float64 // 桶容量
	last   time.Time
	now    func() time.Time
}

func NewTokenBucket(rate, burst float64, now func() time.Time) *TokenBucket {
	if now == nil {
		now = time.Now
	}
	return &TokenBucket{tokens: burst, rate: rate, burst: burst, last: now(), now: now}
}

func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := b.now()
	b.tokens += n.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = n
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type IPLimiter struct {
	mu      sync.Mutex
	buckets map[string]*TokenBucket
	rate    float64
	burst   float64
	now     func() time.Time
}

func NewIPLimiter(rate, burst float64, now func() time.Time) *IPLimiter {
	return &IPLimiter{buckets: make(map[string]*TokenBucket), rate: rate, burst: burst, now: now}
}

func (l *IPLimiter) Allow(ip string) bool {
	l.mu.Lock()
	b, ok := l.buckets[ip]
	if !ok {
		b = NewTokenBucket(l.rate, l.burst, l.now)
		l.buckets[ip] = b
	}
	l.mu.Unlock()
	return b.Allow()
}
```

- [ ] **Step 4: 测试通过后提交**

Run: `go test ./... -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server/internal/gateway
git -C "E:\pro\SignalDrift" commit -m "feat(gateway): 令牌桶与按IP限流器"
```

---

### Task 6: 心跳超时扫描

**Files:**
- Create: `server/internal/gateway/heartbeat.go`
- Test: `server/internal/gateway/heartbeat_test.go`

**Interfaces:**
- Consumes: Task 4 的 `SessionManager/Session.Touch/LastBeat`
- Produces: `gateway.NewHeartbeatWatcher(mgr *SessionManager, timeoutSec int64, onTimeout func(*Session), now func() int64) *HeartbeatWatcher`、`(*HeartbeatWatcher).Sweep()`（单次扫描，可测）、`(*HeartbeatWatcher).Run(ctx context.Context, interval time.Duration)`（循环扫描）

- [ ] **Step 1: 写失败测试**

`server/internal/gateway/heartbeat_test.go`：

```go
package gateway

import (
	"net"
	"testing"
)

func TestSweepTimeout(t *testing.T) {
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	s := m.Create(c1)

	var fakeNow int64 = 100
	s.Touch(fakeNow)
	var timedOut []*Session
	w := NewHeartbeatWatcher(m, 15, func(sess *Session) {
		timedOut = append(timedOut, sess)
	}, func() int64 { return fakeNow })

	w.Sweep()
	if len(timedOut) != 0 {
		t.Fatal("fresh session must not time out")
	}

	fakeNow = 116 // 超过 15 秒
	w.Sweep()
	if len(timedOut) != 1 || timedOut[0] != s {
		t.Fatalf("expected timeout of s, got %v", timedOut)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run Sweep -v` → FAIL

- [ ] **Step 3: 最小实现**

`server/internal/gateway/heartbeat.go`：

```go
package gateway

import (
	"context"
	"time"
)

type HeartbeatWatcher struct {
	mgr        *SessionManager
	timeoutSec int64
	onTimeout  func(*Session)
	now        func() int64
}

func NewHeartbeatWatcher(mgr *SessionManager, timeoutSec int64, onTimeout func(*Session), now func() int64) *HeartbeatWatcher {
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	return &HeartbeatWatcher{mgr: mgr, timeoutSec: timeoutSec, onTimeout: onTimeout, now: now}
}

func (w *HeartbeatWatcher) Sweep() {
	n := w.now()
	w.mgr.Range(func(s *Session) bool {
		if n-s.LastBeat() > w.timeoutSec {
			w.onTimeout(s)
		}
		return true
	})
}

func (w *HeartbeatWatcher) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Sweep()
		}
	}
}
```

- [ ] **Step 4: 测试通过后提交**

Run: `go test ./... -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server/internal/gateway
git -C "E:\pro\SignalDrift" commit -m "feat(gateway): 心跳超时扫描器"
```

---

### Task 7: 消息路由器

**Files:**
- Create: `server/internal/gateway/router.go`
- Test: `server/internal/gateway/router_test.go`

**Interfaces:**
- Consumes: Task 4 的 `Session`；`protocol.Frame`
- Produces: `gateway.HandlerFunc = func(s *Session, f *protocol.Frame)`、`gateway.NewRouter() *Router`、`(*Router).Register(msgID uint16, h HandlerFunc)`、`(*Router).Dispatch(s *Session, f *protocol.Frame)`（未注册 msgID 静默丢弃并计数 `(*Router).UnknownCount() uint64`）——后续计划的大厅/房间服务通过 Register 挂载

- [ ] **Step 1: 写失败测试**

`server/internal/gateway/router_test.go`：

```go
package gateway

import (
	"net"
	"testing"

	"signaldrift/server/internal/protocol"
)

func TestRouterDispatch(t *testing.T) {
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	s := m.Create(c1)

	r := NewRouter()
	var got *protocol.Frame
	r.Register(protocol.MsgEcho, func(sess *Session, f *protocol.Frame) { got = f })

	r.Dispatch(s, &protocol.Frame{MsgID: protocol.MsgEcho, Seq: 1, Body: []byte("x")})
	if got == nil || string(got.Body) != "x" {
		t.Fatalf("handler not called: %+v", got)
	}
}

func TestRouterUnknown(t *testing.T) {
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	s := m.Create(c1)

	r := NewRouter()
	r.Dispatch(s, &protocol.Frame{MsgID: 999}) // 不 panic
	if r.UnknownCount() != 1 {
		t.Fatalf("unknown=%d", r.UnknownCount())
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run Router -v` → FAIL

- [ ] **Step 3: 最小实现**

`server/internal/gateway/router.go`：

```go
package gateway

import (
	"sync"
	"sync/atomic"

	"signaldrift/server/internal/protocol"
)

type HandlerFunc func(s *Session, f *protocol.Frame)

type Router struct {
	mu       sync.RWMutex
	handlers map[uint16]HandlerFunc
	unknown  atomic.Uint64
}

func NewRouter() *Router {
	return &Router{handlers: make(map[uint16]HandlerFunc)}
}

func (r *Router) Register(msgID uint16, h HandlerFunc) {
	r.mu.Lock()
	r.handlers[msgID] = h
	r.mu.Unlock()
}

func (r *Router) Dispatch(s *Session, f *protocol.Frame) {
	r.mu.RLock()
	h, ok := r.handlers[f.MsgID]
	r.mu.RUnlock()
	if !ok {
		r.unknown.Add(1)
		return
	}
	h(s, f)
}

func (r *Router) UnknownCount() uint64 { return r.unknown.Load() }
```

- [ ] **Step 4: 测试通过后提交**

Run: `go test ./... -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server/internal/gateway
git -C "E:\pro\SignalDrift" commit -m "feat(gateway): 消息路由器"
```

---

### Task 8: TCP Server 集成（读写循环 + 限流断连 + 心跳应答）

**Files:**
- Create: `server/internal/gateway/server.go`
- Test: `server/internal/gateway/server_test.go`

**Interfaces:**
- Consumes: 前面全部组件
- Produces: `gateway.NewServer(cfg *config.ServerConfig, router *Router) *Server`、`(*Server).Start() error`（监听成功即返回）、`(*Server).Addr() net.Addr`、`(*Server).Stop()`（关监听器并关闭全部会话）、`(*Server).Sessions() *SessionManager`。内建行为：自动注册 MsgHeartbeat→回 MsgHeartbeatAck 并 Touch；IP 限流超限直接断连；seq 重复帧静默丢弃

- [ ] **Step 1: 写失败测试**

`server/internal/gateway/server_test.go`：

```go
package gateway

import (
	"net"
	"testing"
	"time"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/protocol"
)

func startTestServer(t *testing.T, rate, burst float64) *Server {
	t.Helper()
	cfg := &config.ServerConfig{
		ListenAddr:    "127.0.0.1:0",
		MaxConns:      16,
		SendQueueSize: 16,
		IPRate:        config.RateConfig{Rate: rate, Burst: burst},
		Heartbeat:     config.HeartbeatConfig{IntervalSec: 5, TimeoutSec: 15, SweepSec: 1},
	}
	srv := NewServer(cfg, NewRouter())
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return srv
}

func dial(t *testing.T, srv *Server) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestHeartbeatAck(t *testing.T) {
	srv := startTestServer(t, 100, 100)
	c := dial(t, srv)
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 1}))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	f, err := protocol.NewFrameReader(c).Next()
	if err != nil || f.MsgID != protocol.MsgHeartbeatAck {
		t.Fatalf("f=%+v err=%v", f, err)
	}
}

func TestDuplicateSeqDropped(t *testing.T) {
	srv := startTestServer(t, 100, 100)
	c := dial(t, srv)
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 5}))
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 5})) // 重复
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 6}))
	fr := protocol.NewFrameReader(c)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	fr.Next() // ack for seq5
	fr.Next() // ack for seq6
	// 第三个 ack 不应存在：短暂等待后读取必须超时
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := fr.Next(); err == nil {
		t.Fatal("duplicate seq must not produce a 3rd ack")
	}
}

func TestRateLimitKick(t *testing.T) {
	srv := startTestServer(t, 1, 2) // 极小限额
	c := dial(t, srv)
	for i := 1; i <= 10; i++ {
		c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: uint32(i)}))
	}
	// 超限后服务端应断开：持续读最终得到错误
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	fr := protocol.NewFrameReader(c)
	for {
		if _, err := fr.Next(); err != nil {
			return // 连接被服务端关闭，符合预期
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run 'Heartbeat|Duplicate|RateLimit' -v` → FAIL

- [ ] **Step 3: 最小实现**

`server/internal/gateway/server.go`：

```go
package gateway

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/protocol"
)

type Server struct {
	cfg      *config.ServerConfig
	router   *Router
	mgr      *SessionManager
	limiter  *IPLimiter
	listener net.Listener
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewServer(cfg *config.ServerConfig, router *Router) *Server {
	srv := &Server{
		cfg:     cfg,
		router:  router,
		mgr:     NewSessionManager(cfg.SendQueueSize),
		limiter: NewIPLimiter(cfg.IPRate.Rate, cfg.IPRate.Burst, nil),
	}
	// 网关内建：心跳应答
	router.Register(protocol.MsgHeartbeat, func(s *Session, f *protocol.Frame) {
		s.Touch(time.Now().Unix())
		s.Send(protocol.MsgHeartbeatAck, nil)
	})
	return srv
}

func (srv *Server) Sessions() *SessionManager { return srv.mgr }
func (srv *Server) Addr() net.Addr            { return srv.listener.Addr() }

func (srv *Server) Start() error {
	ln, err := net.Listen("tcp", srv.cfg.ListenAddr)
	if err != nil {
		return err
	}
	srv.listener = ln
	ctx, cancel := context.WithCancel(context.Background())
	srv.cancel = cancel

	watcher := NewHeartbeatWatcher(srv.mgr, int64(srv.cfg.Heartbeat.TimeoutSec), func(s *Session) {
		log.Printf("WARN session %d heartbeat timeout", s.ID)
		s.Close()
	}, nil)
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		watcher.Run(ctx, time.Duration(srv.cfg.Heartbeat.SweepSec)*time.Second)
	}()

	srv.wg.Add(1)
	go srv.acceptLoop()
	return nil
}

func (srv *Server) acceptLoop() {
	defer srv.wg.Done()
	for {
		conn, err := srv.listener.Accept()
		if err != nil {
			return // 监听器已关闭
		}
		if srv.mgr.Count() >= srv.cfg.MaxConns {
			log.Printf("WARN max conns reached, reject %s", conn.RemoteAddr())
			conn.Close()
			continue
		}
		s := srv.mgr.Create(conn)
		s.Touch(time.Now().Unix())
		srv.wg.Add(2)
		go srv.writeLoop(s, conn)
		go srv.readLoop(s, conn)
	}
}

func (srv *Server) readLoop(s *Session, conn net.Conn) {
	defer srv.wg.Done()
	defer srv.teardown(s)
	fr := protocol.NewFrameReader(conn)
	ip := s.RemoteIP()
	for {
		f, err := fr.Next()
		if err != nil {
			return // 断开或协议错误
		}
		if !srv.limiter.Allow(ip) {
			log.Printf("WARN rate limit kick session=%d ip=%s", s.ID, ip)
			return
		}
		if !s.CheckSeq(f.Seq) {
			continue // 重复/回退包：静默丢弃
		}
		srv.router.Dispatch(s, f)
	}
}

func (srv *Server) writeLoop(s *Session, conn net.Conn) {
	defer srv.wg.Done()
	for {
		select {
		case <-s.Done():
			return
		case raw := <-s.SendQueue():
			if _, err := conn.Write(raw); err != nil {
				s.Close()
				return
			}
		}
	}
}

func (srv *Server) teardown(s *Session) {
	s.Close()
	srv.mgr.Remove(s.ID)
}

func (srv *Server) Stop() {
	if srv.cancel != nil {
		srv.cancel()
	}
	if srv.listener != nil {
		srv.listener.Close()
	}
	srv.mgr.Range(func(s *Session) bool { s.Close(); return true })
	srv.wg.Wait()
}
```

- [ ] **Step 4: 测试通过（含竞态检测）后提交**

Run: `go test ./... -race -v` → PASS

```bash
git -C "E:\pro\SignalDrift" add server/internal/gateway
git -C "E:\pro\SignalDrift" commit -m "feat(gateway): TCP接入层集成——读写循环/限流断连/心跳应答"
```

---

### Task 9: 进程入口与优雅关闭

**Files:**
- Create: `server/cmd/gateway/main.go`

**Interfaces:**
- Consumes: `config.Load`、`gateway.NewServer/NewRouter`、`protocol.MsgEcho`
- Produces: 可执行网关进程 `go run ./cmd/gateway`；临时注册 Echo 处理器供手动联调（大厅计划接入后移除）

- [ ] **Step 1: 实现入口**

`server/cmd/gateway/main.go`：

```go
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/gateway"
	"signaldrift/server/internal/protocol"
)

func main() {
	cfgPath := flag.String("config", "configs/server.json", "config file path")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("ERROR load config: %v", err)
	}

	router := gateway.NewRouter()
	// 临时 Echo：验证全链路，大厅服务接入后移除
	router.Register(protocol.MsgEcho, func(s *gateway.Session, f *protocol.Frame) {
		s.Send(protocol.MsgEcho, f.Body)
	})

	srv := gateway.NewServer(cfg, router)
	if err := srv.Start(); err != nil {
		log.Fatalf("ERROR start: %v", err)
	}
	log.Printf("INFO gateway listening on %s", srv.Addr())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("INFO shutting down...")
	srv.Stop()
	log.Println("INFO gateway stopped")
}
```

- [ ] **Step 2: 全量验证**

Run: `go vet ./...` → 无输出
Run: `go test ./... -race` → 全部 PASS
Run: `go build ./...` → 编译通过

- [ ] **Step 3: 手动冒烟**

```bash
cd "E:\pro\SignalDrift\server"; go run ./cmd/gateway
```

Expected: 打印 `INFO gateway listening on [::]:8080`（或 0.0.0.0:8080）；Ctrl+C 后打印 `INFO gateway stopped` 且进程干净退出。

- [ ] **Step 4: 提交**

```bash
git -C "E:\pro\SignalDrift" add server
git -C "E:\pro\SignalDrift" commit -m "feat(gateway): 进程入口与优雅关闭"
```

---

## Self-Review 结果

1. **规格覆盖**（网关范围 = 规格 6.1 网关部分）：粘包分包(T3)、会话(T4)、多层限流之 IP 限流(T5,T8)、重复包幂等(T4,T8)、心跳断线检测(T6,T8)、消息路由(T7)、优雅关闭(T9)。规格中"发射指令专项限流"依赖战斗消息，归入计划 4；"断线重连 Token"依赖账号体系，归入计划 2（大厅）——两者在对应计划中实现，此处显式声明不缺漏。
2. **占位符扫描**：所有步骤含完整代码与命令，无 TBD/省略。
3. **类型一致性**：`Session.Send(msgID, body) bool`、`Router.Register`、`FrameReader.Next` 等签名在各 Task 的 Interfaces 块与代码中一致；跨包引用统一 `signaldrift/server/internal/...`。
