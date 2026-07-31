# 《信号漂流》计划 3/5：Unity 客户端（网络层与大厅）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unity 客户端地基：C# 二进制帧编解码（与 Go 端字节级一致）、TCP 网络客户端（后台收发线程 + 主线程消息队列）、心跳、登录/注册 UI、大厅 UI（匹配/取消/档案/好友）、匹配成功跳转战斗场景占位。

**Architecture:** `Assets/Scripts/Net`（纯 C# 无 UnityEngine 依赖的协议层 + NetworkClient MonoBehaviour 单例）与 `Assets/Scripts/UI`（场景脚本）分离。协议层用 Unity Test Framework EditMode 单测（可 TDD）；MonoBehaviour/场景用手动冒烟验收（对着计划 2 已跑起来的服务端）。战斗场景的渲染与输入在计划 4 实现，本计划只建占位场景。

**Tech Stack:** Unity 6 (URP 2D)，项目 `E:\unity_xiangmu\SignalDrift`；仅内置包（Input System、TextMeshPro、Test Framework），不引第三方网络库（自研对齐 Go 端是卖点）。

**前置:** 计划 1、2 已完成（服务端可注册/登录/匹配）。

## Global Constraints

- 与服务端字节级约定：**大端序**；帧 = `magic(2B)=0x5344 | msgID(2B) | seq(4B) | bodyLen(4B) | body`；body 为 UTF-8 JSON
- msgID 常量必须与 Go 端 `protocol/msgid.go` 数值一一相等（心跳 1/2、大厅 200-299）
- 所有网络回调消息只在主线程消费（Update 泵队列），MonoBehaviour 严禁被后台线程触碰
- 客户端不做任何战斗逻辑（规格铁律）
- UI 极简风：默认 TMP 字体、深底(#0B0F1A)亮字(#E2E8F0)、青色强调(#22D3EE)，不引美术资源
- 协议层代码（`Net/Protocol` 目录）不得 `using UnityEngine`，保证可被 EditMode 测试与未来控制台压测工具复用
- 每 Task 完成后 EditMode 测试全绿（有测试的 Task）再提交；Unity 项目单独 git 仓库（见 Task 1）

## File Structure

```
SignalDrift/Assets/
  Scripts/
    Net/Protocol/FrameCodec.cs      — 帧编码/解码器（含粘包缓冲）
    Net/Protocol/MsgId.cs           — 消息 ID 常量（对齐 Go）
    Net/Protocol/Messages.cs        — JSON DTO（对齐 lobby/dto.go）
    Net/NetworkClient.cs            — TCP 客户端单例（连接/收发/心跳/重连字段）
    Net/MessageDispatcher.cs        — msgID→handler 注册分发
    UI/LoginController.cs           — 登录/注册界面
    UI/LobbyController.cs           — 大厅界面（匹配/档案/好友）
    UI/UiTheme.cs                   — 颜色常量
    Game/BattleSceneStub.cs         — 战斗场景占位（显示 RoomID）
  Scenes/
    Login.unity / Lobby.unity / Battle.unity
  Tests/EditMode/
    SignalDrift.Tests.asmdef
    FrameCodecTests.cs
    MessagesTests.cs
```

---

### Task 1: Unity 项目 git 初始化与目录骨架

**Files:**
- Create: `SignalDrift/.gitignore`
- Create: 上述 Scripts 目录结构（空文件夹由后续任务填充）

- [ ] **Step 1: git 初始化**

```bash
git -C "E:\unity_xiangmu\SignalDrift" init
```

`.gitignore`（Unity 标准）：

```gitignore
[Ll]ibrary/
[Tt]emp/
[Oo]bj/
[Bb]uild/
[Bb]uilds/
[Ll]ogs/
[Uu]serSettings/
*.csproj
*.sln
*.pidb.meta
crashlytics-build.properties
```

- [ ] **Step 2: 首次提交**

```bash
git -C "E:\unity_xiangmu\SignalDrift" add -A
git -C "E:\unity_xiangmu\SignalDrift" commit -m "chore: Unity URP2D 模板初始提交与gitignore"
```

---

### Task 2: FrameCodec（C# 帧编解码，EditMode TDD）

**Files:**
- Create: `Assets/Scripts/Net/Protocol/FrameCodec.cs`、`MsgId.cs`
- Create: `Assets/Tests/EditMode/SignalDrift.Tests.asmdef`（引用主 asmdef 或 Assembly-CSharp）
- Test: `Assets/Tests/EditMode/FrameCodecTests.cs`

**Interfaces:**
- Produces:

```csharp
public struct Frame { public ushort MsgId; public uint Seq; public byte[] Body; }
public static class FrameCodec {
    public const ushort Magic = 0x5344;
    public const int HeaderSize = 12;
    public const int MaxBodySize = 65536;
    public static byte[] Encode(ushort msgId, uint seq, byte[] body);
    // 追加字节到内部缓冲；每解出一个完整帧回调 onFrame；魔数错误抛 ProtocolException
}
public class FrameDecoder {
    public void Feed(byte[] data, int len, Action<Frame> onFrame);
}
public static class MsgId {
    public const ushort Heartbeat = 1; public const ushort HeartbeatAck = 2;
    public const ushort RegisterReq = 200; /* ...与 Go 端逐一对齐，含 299 */
}
```

- [ ] **Step 1: 写失败测试**（`FrameCodecTests.cs`）

```csharp
using System;
using System.Collections.Generic;
using NUnit.Framework;

public class FrameCodecTests
{
    [Test]
    public void Encode_Layout_BigEndian()
    {
        var body = new byte[] { 0x61, 0x62, 0x63 };
        var b = FrameCodec.Encode(7, 42, body);
        Assert.AreEqual(FrameCodec.HeaderSize + 3, b.Length);
        Assert.AreEqual(0x53, b[0]); Assert.AreEqual(0x44, b[1]);          // magic
        Assert.AreEqual(0x00, b[2]); Assert.AreEqual(0x07, b[3]);          // msgId
        Assert.AreEqual(42, (b[4] << 24) | (b[5] << 16) | (b[6] << 8) | b[7]); // seq
        Assert.AreEqual(3, (b[8] << 24) | (b[9] << 16) | (b[10] << 8) | b[11]); // len
        Assert.AreEqual(0x61, b[12]);
    }

    [Test]
    public void Decoder_MultiFrame_And_Fragmented()
    {
        var f1 = FrameCodec.Encode(1, 1, new byte[] { 1, 2 });
        var f2 = FrameCodec.Encode(2, 2, new byte[] { 3 });
        var all = new byte[f1.Length + f2.Length];
        Buffer.BlockCopy(f1, 0, all, 0, f1.Length);
        Buffer.BlockCopy(f2, 0, all, f1.Length, f2.Length);

        var got = new List<Frame>();
        var dec = new FrameDecoder();
        // 一个字节一个字节喂，模拟极端拆包
        for (int i = 0; i < all.Length; i++)
            dec.Feed(new[] { all[i] }, 1, f => got.Add(f));

        Assert.AreEqual(2, got.Count);
        Assert.AreEqual(1, got[0].MsgId);
        Assert.AreEqual(2, got[0].Body.Length);
        Assert.AreEqual(2, got[1].MsgId);
        Assert.AreEqual(2u, got[1].Seq);
    }

    [Test]
    public void Decoder_BadMagic_Throws()
    {
        var raw = FrameCodec.Encode(1, 1, null);
        raw[0] = 0xFF;
        var dec = new FrameDecoder();
        Assert.Throws<ProtocolException>(() => dec.Feed(raw, raw.Length, _ => { }));
    }
}
```

- [ ] **Step 2: Unity Test Runner 运行确认失败**（EditMode，编译错误即视为 FAIL）

- [ ] **Step 3: 实现 `FrameCodec.cs`**

```csharp
using System;

public class ProtocolException : Exception
{
    public ProtocolException(string msg) : base(msg) { }
}

public struct Frame
{
    public ushort MsgId;
    public uint Seq;
    public byte[] Body;
}

public static class FrameCodec
{
    public const ushort Magic = 0x5344;
    public const int HeaderSize = 12;
    public const int MaxBodySize = 65536;

    public static byte[] Encode(ushort msgId, uint seq, byte[] body)
    {
        int bodyLen = body?.Length ?? 0;
        var b = new byte[HeaderSize + bodyLen];
        b[0] = (byte)(Magic >> 8); b[1] = (byte)Magic;
        b[2] = (byte)(msgId >> 8); b[3] = (byte)msgId;
        b[4] = (byte)(seq >> 24); b[5] = (byte)(seq >> 16);
        b[6] = (byte)(seq >> 8); b[7] = (byte)seq;
        b[8] = (byte)(bodyLen >> 24); b[9] = (byte)(bodyLen >> 16);
        b[10] = (byte)(bodyLen >> 8); b[11] = (byte)bodyLen;
        if (bodyLen > 0) Buffer.BlockCopy(body, 0, b, HeaderSize, bodyLen);
        return b;
    }
}

public class FrameDecoder
{
    private byte[] _buf = new byte[8192];
    private int _len;

    public void Feed(byte[] data, int count, Action<Frame> onFrame)
    {
        EnsureCapacity(_len + count);
        Buffer.BlockCopy(data, 0, _buf, _len, count);
        _len += count;

        int offset = 0;
        while (_len - offset >= FrameCodec.HeaderSize)
        {
            int magic = (_buf[offset] << 8) | _buf[offset + 1];
            if (magic != FrameCodec.Magic)
                throw new ProtocolException("bad magic");
            int bodyLen = (_buf[offset + 8] << 24) | (_buf[offset + 9] << 16)
                        | (_buf[offset + 10] << 8) | _buf[offset + 11];
            if (bodyLen > FrameCodec.MaxBodySize)
                throw new ProtocolException("body too large");
            if (_len - offset < FrameCodec.HeaderSize + bodyLen) break; // 半包，等下次

            var f = new Frame
            {
                MsgId = (ushort)((_buf[offset + 2] << 8) | _buf[offset + 3]),
                Seq = (uint)((_buf[offset + 4] << 24) | (_buf[offset + 5] << 16)
                           | (_buf[offset + 6] << 8) | _buf[offset + 7]),
                Body = new byte[bodyLen],
            };
            Buffer.BlockCopy(_buf, offset + FrameCodec.HeaderSize, f.Body, 0, bodyLen);
            offset += FrameCodec.HeaderSize + bodyLen;
            onFrame(f);
        }
        if (offset > 0)
        {
            Buffer.BlockCopy(_buf, offset, _buf, 0, _len - offset);
            _len -= offset;
        }
    }

    private void EnsureCapacity(int need)
    {
        if (need <= _buf.Length) return;
        int cap = _buf.Length * 2;
        while (cap < need) cap *= 2;
        Array.Resize(ref _buf, cap);
    }
}
```

`MsgId.cs`（数值必须与 Go 端逐一核对）：

```csharp
public static class MsgId
{
    public const ushort Heartbeat = 1;
    public const ushort HeartbeatAck = 2;
    public const ushort RegisterReq = 200;
    public const ushort RegisterResp = 201;
    public const ushort LoginReq = 202;
    public const ushort LoginResp = 203;
    public const ushort MatchReq = 210;
    public const ushort MatchResp = 211;
    public const ushort MatchCancel = 212;
    public const ushort MatchCancelOK = 213;
    public const ushort MatchFound = 215;
    public const ushort FriendAdd = 220;
    public const ushort FriendAddOK = 221;
    public const ushort FriendDel = 222;
    public const ushort FriendDelOK = 223;
    public const ushort FriendList = 224;
    public const ushort FriendListOK = 225;
    public const ushort ProfileReq = 230;
    public const ushort ProfileResp = 231;
    public const ushort EloUpdate = 234;
    public const ushort ErrorResp = 299;
}
```

- [ ] **Step 4: EditMode 测试全绿后提交**

```bash
git -C "E:\unity_xiangmu\SignalDrift" add Assets
git -C "E:\unity_xiangmu\SignalDrift" commit -m "feat(net): C#帧编解码与粘包解码器(EditMode测试)"
```

---

### Task 3: 消息 DTO 与 JSON 序列化

**Files:**
- Create: `Assets/Scripts/Net/Protocol/Messages.cs`
- Test: `Assets/Tests/EditMode/MessagesTests.cs`

**Interfaces:**
- Produces: `[Serializable]` 类：`RegisterReq{username,password}`、`RegisterResp{code,uid}`、`LoginReq`、`LoginResp{code,uid,elo,token}`、`MatchFoundPush{room_id,opp_uid,opp_elo}`、`FriendAddReq{friend_uid}`、`FriendInfo{uid,elo,online}`、`FriendListResp{code,friends}`、`ProfileResp{code,uid,elo,max_elo,wins,losses}`、`ErrorResp{code,msg}`；静态类 `Json { static byte[] Ser<T>(T v); static T De<T>(byte[] body); }`（UTF-8 + JsonUtility）

字段名必须与 Go 端 JSON tag 完全一致（小写下划线）。JsonUtility 不支持属性重命名 → **C# 字段名直接用下划线命名**（如 `public long room_id;`），这是与 Go JSON 对齐的最低成本决策。

- [ ] **Step 1: 写失败测试**

```csharp
using NUnit.Framework;
using System.Text;

public class MessagesTests
{
    [Test]
    public void LoginResp_Roundtrip_GoJson()
    {
        // Go 端真实输出样例
        var goJson = "{\"code\":0,\"uid\":7,\"elo\":1000,\"token\":\"7.99.abc\"}";
        var v = Json.De<LoginResp>(Encoding.UTF8.GetBytes(goJson));
        Assert.AreEqual(0, v.code);
        Assert.AreEqual(7, v.uid);
        Assert.AreEqual(1000, v.elo);
        Assert.AreEqual("7.99.abc", v.token);
    }

    [Test]
    public void RegisterReq_Serialize()
    {
        var raw = Json.Ser(new RegisterReq { username = "alice", password = "123456" });
        var s = Encoding.UTF8.GetString(raw);
        StringAssert.Contains("\"username\":\"alice\"", s);
        StringAssert.Contains("\"password\":\"123456\"", s);
    }

    [Test]
    public void MatchFound_Deserialize()
    {
        var goJson = "{\"room_id\":33,\"opp_uid\":9,\"opp_elo\":1080}";
        var v = Json.De<MatchFoundPush>(Encoding.UTF8.GetBytes(goJson));
        Assert.AreEqual(33, v.room_id);
        Assert.AreEqual(9, v.opp_uid);
    }
}
```

- [ ] **Step 2: 确认失败 → 实现 `Messages.cs`**

```csharp
using System;
using System.Text;
using UnityEngine;

[Serializable] public class ErrorResp { public int code; public string msg; }
[Serializable] public class RegisterReq { public string username; public string password; }
[Serializable] public class RegisterResp { public int code; public long uid; }
[Serializable] public class LoginReq { public string username; public string password; }
[Serializable] public class LoginResp { public int code; public long uid; public int elo; public string token; }
[Serializable] public class MatchFoundPush { public long room_id; public long opp_uid; public int opp_elo; }
[Serializable] public class FriendAddReq { public long friend_uid; }
[Serializable] public class FriendDelReq { public long friend_uid; }
[Serializable] public class FriendInfo { public long uid; public int elo; public bool online; }
[Serializable] public class FriendListResp { public int code; public FriendInfo[] friends; }
[Serializable] public class ProfileResp { public int code; public long uid; public int elo; public int max_elo; public int wins; public int losses; }

public static class Json
{
    public static byte[] Ser<T>(T v) => Encoding.UTF8.GetBytes(JsonUtility.ToJson(v));
    public static T De<T>(byte[] body) => JsonUtility.FromJson<T>(Encoding.UTF8.GetString(body));
}
```

注意：`Messages.cs` 因用 JsonUtility 需要 UnityEngine——把"无 UnityEngine"约束限定在 `FrameCodec.cs/MsgId.cs`（Global Constraints 相应收窄为 Protocol 编解码文件）。

- [ ] **Step 3: 测试全绿后提交**

```bash
git -C "E:\unity_xiangmu\SignalDrift" add Assets
git -C "E:\unity_xiangmu\SignalDrift" commit -m "feat(net): 消息DTO与Go端JSON字段对齐(EditMode测试)"
```

---

### Task 4: NetworkClient 单例与消息分发

**Files:**
- Create: `Assets/Scripts/Net/NetworkClient.cs`、`Assets/Scripts/Net/MessageDispatcher.cs`

**Interfaces:**
- Produces:

```csharp
public class NetworkClient : MonoBehaviour {   // DontDestroyOnLoad 单例 NetworkClient.I
    public bool Connected { get; }
    public long Uid;          // 登录后由 LoginController 写入
    public string ReconnectToken;
    public void Connect(string host, int port, Action<bool> onResult);
    public void Send<T>(ushort msgId, T body);   // 自动 seq 自增
    public void SendEmpty(ushort msgId);
    public MessageDispatcher Dispatcher { get; }
    public event Action OnDisconnected;          // 主线程触发
}
public class MessageDispatcher {
    public void On(ushort msgId, Action<byte[]> handler);   // 覆盖式注册
    public void Off(ushort msgId);
    public void Dispatch(ushort msgId, byte[] body);
}
```

- [ ] **Step 1: 实现 `MessageDispatcher.cs`**

```csharp
using System;
using System.Collections.Generic;

public class MessageDispatcher
{
    private readonly Dictionary<ushort, Action<byte[]>> _handlers = new();

    public void On(ushort msgId, Action<byte[]> handler) => _handlers[msgId] = handler;
    public void Off(ushort msgId) => _handlers.Remove(msgId);

    public void Dispatch(ushort msgId, byte[] body)
    {
        if (_handlers.TryGetValue(msgId, out var h)) h(body);
        else UnityEngine.Debug.LogWarning($"[Net] no handler for msgId={msgId}");
    }
}
```

- [ ] **Step 2: 实现 `NetworkClient.cs`**

```csharp
using System;
using System.Collections.Concurrent;
using System.Net.Sockets;
using System.Threading;
using UnityEngine;

public class NetworkClient : MonoBehaviour
{
    public static NetworkClient I { get; private set; }

    public bool Connected => _client?.Connected ?? false;
    public long Uid;
    public string ReconnectToken;
    public MessageDispatcher Dispatcher { get; } = new();
    public event Action OnDisconnected;

    private TcpClient _client;
    private NetworkStream _stream;
    private Thread _recvThread;
    private readonly ConcurrentQueue<Frame> _inbox = new();
    private readonly object _sendLock = new();
    private uint _seq;
    private volatile bool _running;
    private float _heartbeatTimer;
    private bool _disconnectPending;

    private void Awake()
    {
        if (I != null) { Destroy(gameObject); return; }
        I = this;
        DontDestroyOnLoad(gameObject);
    }

    public void Connect(string host, int port, Action<bool> onResult)
    {
        Disconnect();
        try
        {
            _client = new TcpClient();
            _client.NoDelay = true;
            _client.Connect(host, port);
            _stream = _client.GetStream();
            _running = true;
            _recvThread = new Thread(RecvLoop) { IsBackground = true };
            _recvThread.Start();
            onResult(true);
        }
        catch (Exception e)
        {
            Debug.LogError($"[Net] connect failed: {e.Message}");
            onResult(false);
        }
    }

    private void RecvLoop()
    {
        var dec = new FrameDecoder();
        var buf = new byte[8192];
        try
        {
            while (_running)
            {
                int n = _stream.Read(buf, 0, buf.Length);
                if (n <= 0) break;
                dec.Feed(buf, n, f => _inbox.Enqueue(f));
            }
        }
        catch (Exception) { /* 连接中断，统一走断线流程 */ }
        _disconnectPending = true;
    }

    public void Send<T>(ushort msgId, T body) => SendRaw(msgId, Json.Ser(body));
    public void SendEmpty(ushort msgId) => SendRaw(msgId, null);

    private void SendRaw(ushort msgId, byte[] body)
    {
        if (!Connected) return;
        var seq = ++_seq;
        var raw = FrameCodec.Encode(msgId, seq, body);
        lock (_sendLock)
        {
            try { _stream.Write(raw, 0, raw.Length); }
            catch (Exception e) { Debug.LogError($"[Net] send: {e.Message}"); }
        }
    }

    private void Update()
    {
        // 主线程泵消息
        while (_inbox.TryDequeue(out var f))
        {
            if (f.MsgId == MsgId.HeartbeatAck) continue;
            Dispatcher.Dispatch(f.MsgId, f.Body);
        }
        // 心跳（5 秒，与服务端 configs 一致）
        if (Connected)
        {
            _heartbeatTimer += Time.unscaledDeltaTime;
            if (_heartbeatTimer >= 5f)
            {
                _heartbeatTimer = 0f;
                SendEmpty(MsgId.Heartbeat);
            }
        }
        if (_disconnectPending)
        {
            _disconnectPending = false;
            Disconnect();
            OnDisconnected?.Invoke();
        }
    }

    public void Disconnect()
    {
        _running = false;
        _stream?.Close();
        _client?.Close();
        _stream = null;
        _client = null;
    }

    private void OnDestroy() => Disconnect();
}
```

- [ ] **Step 3: 编译验证**（Unity 无编译错误、EditMode 测试仍全绿）

- [ ] **Step 4: 提交**

```bash
git -C "E:\unity_xiangmu\SignalDrift" add Assets
git -C "E:\unity_xiangmu\SignalDrift" commit -m "feat(net): TCP客户端单例——收发线程/主线程泵/心跳/断线事件"
```

---

### Task 5: 登录场景（Login.unity + LoginController）

**Files:**
- Create: `Assets/Scenes/Login.unity`、`Assets/Scripts/UI/LoginController.cs`、`Assets/Scripts/UI/UiTheme.cs`

**Interfaces:**
- Consumes: NetworkClient、MsgId、DTO
- Produces: 可运行的登录场景；登录成功 → `SceneManager.LoadScene("Lobby")`

场景搭建（Editor 手工，步骤明确）：
1. 新建场景 Login：Canvas(Scale With Screen Size 1920×1080) + 背景 Image(#0B0F1A)
2. 中央垂直布局：标题 TMP"SIGNAL DRIFT"(#22D3EE, 72pt)、输入框 username、输入框 password(Content Type: Password)、按钮【登录】、按钮【注册】、状态文本 statusText(#F87171)
3. 空物体 `Network` 挂 NetworkClient；空物体 `LoginUI` 挂 LoginController 并拖引用
4. Build Settings 加入 Login/Lobby/Battle 三场景，Login 为 0

- [ ] **Step 1: `UiTheme.cs`**

```csharp
using UnityEngine;

public static class UiTheme
{
    public static readonly Color Bg = new Color32(0x0B, 0x0F, 0x1A, 0xFF);
    public static readonly Color Text = new Color32(0xE2, 0xE8, 0xF0, 0xFF);
    public static readonly Color Accent = new Color32(0x22, 0xD3, 0xEE, 0xFF);
    public static readonly Color Accent2 = new Color32(0xF4, 0x72, 0xB6, 0xFF);
    public static readonly Color Error = new Color32(0xF8, 0x71, 0x71, 0xFF);
}
```

- [ ] **Step 2: `LoginController.cs`**

```csharp
using TMPro;
using UnityEngine;
using UnityEngine.SceneManagement;
using UnityEngine.UI;

public class LoginController : MonoBehaviour
{
    [SerializeField] private TMP_InputField usernameInput;
    [SerializeField] private TMP_InputField passwordInput;
    [SerializeField] private Button loginButton;
    [SerializeField] private Button registerButton;
    [SerializeField] private TMP_Text statusText;

    [SerializeField] private string host = "127.0.0.1";
    [SerializeField] private int port = 8080;

    private void Start()
    {
        loginButton.onClick.AddListener(() => Submit(isRegister: false));
        registerButton.onClick.AddListener(() => Submit(isRegister: true));

        var d = NetworkClient.I.Dispatcher;
        d.On(MsgId.RegisterResp, body =>
        {
            var r = Json.De<RegisterResp>(body);
            statusText.text = r.code == 0 ? $"注册成功 UID={r.uid}，请登录"
                : r.code == 409 ? "用户名已存在" : $"注册失败({r.code})";
        });
        d.On(MsgId.LoginResp, body =>
        {
            var r = Json.De<LoginResp>(body);
            if (r.code != 0) { statusText.text = $"登录失败({r.code})"; return; }
            NetworkClient.I.Uid = r.uid;
            NetworkClient.I.ReconnectToken = r.token;
            SceneManager.LoadScene("Lobby");
        });
    }

    private void Submit(bool isRegister)
    {
        var u = usernameInput.text.Trim();
        var p = passwordInput.text;
        if (u.Length < 3 || p.Length < 6) { statusText.text = "用户名≥3字符，密码≥6字符"; return; }

        void DoSend()
        {
            if (isRegister) NetworkClient.I.Send(MsgId.RegisterReq, new RegisterReq { username = u, password = p });
            else NetworkClient.I.Send(MsgId.LoginReq, new LoginReq { username = u, password = p });
        }

        if (!NetworkClient.I.Connected)
        {
            statusText.text = "连接中...";
            NetworkClient.I.Connect(host, port, ok =>
            {
                statusText.text = ok ? "" : "无法连接服务器";
                if (ok) DoSend();
            });
        }
        else DoSend();
    }

    private void OnDestroy()
    {
        var d = NetworkClient.I?.Dispatcher;
        d?.Off(MsgId.RegisterResp);
        d?.Off(MsgId.LoginResp);
    }
}
```

- [ ] **Step 3: 手动冒烟验收**（服务端跑起：MySQL + `go run ./cmd/gateway`）
  - Play → 注册新号 → 状态显示"注册成功"
  - 登录 → 跳转 Lobby 场景（空场景亦可，Task 6 填充）
  - 错密码 → 显示"登录失败(403)"
  - 服务端日志无 ERROR

- [ ] **Step 4: 提交**

```bash
git -C "E:\unity_xiangmu\SignalDrift" add Assets ProjectSettings
git -C "E:\unity_xiangmu\SignalDrift" commit -m "feat(ui): 登录/注册场景与服务端联调通过"
```

---

### Task 6: 大厅场景（匹配/档案/好友）

**Files:**
- Create: `Assets/Scenes/Lobby.unity`、`Assets/Scripts/UI/LobbyController.cs`

**Interfaces:**
- Consumes: NetworkClient、DTO
- Produces: 大厅场景；收到 MatchFound 后把 `room_id` 存入静态 `BattleContext` 并加载 Battle 场景

场景搭建：
1. Canvas 深底；左侧面板：玩家名/UID/ELO/胜负场（ProfileResp 填充）；中央大按钮【开始匹配】与【取消匹配】（互斥显隐）+ 匹配计时文本；右侧好友面板：好友 UID 输入框+【添加】、好友列表（ScrollView，每行 UID/ELO/在线圆点）
2. `LobbyUI` 挂 LobbyController 拖引用

- [ ] **Step 1: `BattleContext.cs`**（放 `Assets/Scripts/Game/`）

```csharp
public static class BattleContext
{
    public static long RoomId;
    public static long OpponentUid;
    public static int OpponentElo;
}
```

- [ ] **Step 2: `LobbyController.cs`**

```csharp
using System.Text;
using TMPro;
using UnityEngine;
using UnityEngine.SceneManagement;
using UnityEngine.UI;

public class LobbyController : MonoBehaviour
{
    [SerializeField] private TMP_Text profileText;
    [SerializeField] private Button matchButton;
    [SerializeField] private Button cancelButton;
    [SerializeField] private TMP_Text matchStatusText;
    [SerializeField] private TMP_InputField friendUidInput;
    [SerializeField] private Button friendAddButton;
    [SerializeField] private TMP_Text friendListText; // 一期用整块文本渲染列表，够用

    private float _matchTimer = -1f;

    private void Start()
    {
        matchButton.onClick.AddListener(() =>
        {
            NetworkClient.I.SendEmpty(MsgId.MatchReq);
        });
        cancelButton.onClick.AddListener(() =>
        {
            NetworkClient.I.SendEmpty(MsgId.MatchCancel);
        });
        friendAddButton.onClick.AddListener(() =>
        {
            if (long.TryParse(friendUidInput.text, out var fuid))
                NetworkClient.I.Send(MsgId.FriendAdd, new FriendAddReq { friend_uid = fuid });
        });

        var d = NetworkClient.I.Dispatcher;
        d.On(MsgId.ProfileResp, body =>
        {
            var p = Json.De<ProfileResp>(body);
            profileText.text = $"UID {p.uid}\nELO {p.elo}  (最高 {p.max_elo})\n{p.wins}胜 {p.losses}负";
        });
        d.On(MsgId.MatchResp, body =>
        {
            var r = Json.De<ErrorResp>(body);
            if (r.code == 0) { _matchTimer = 0f; SetMatching(true); }
            else matchStatusText.text = $"匹配请求失败({r.code})";
        });
        d.On(MsgId.MatchCancelOK, _ => { _matchTimer = -1f; SetMatching(false); });
        d.On(MsgId.MatchFound, body =>
        {
            var mf = Json.De<MatchFoundPush>(body);
            BattleContext.RoomId = mf.room_id;
            BattleContext.OpponentUid = mf.opp_uid;
            BattleContext.OpponentElo = mf.opp_elo;
            SceneManager.LoadScene("Battle");
        });
        d.On(MsgId.FriendAddOK, _ => NetworkClient.I.SendEmpty(MsgId.FriendList));
        d.On(MsgId.FriendListOK, body =>
        {
            var fl = Json.De<FriendListResp>(body);
            var sb = new StringBuilder();
            if (fl.friends != null)
                foreach (var f in fl.friends)
                    sb.AppendLine($"{(f.online ? "●" : "○")} UID {f.uid}  ELO {f.elo}");
            friendListText.text = sb.Length > 0 ? sb.ToString() : "暂无好友";
        });

        NetworkClient.I.SendEmpty(MsgId.ProfileReq);
        NetworkClient.I.SendEmpty(MsgId.FriendList);
        SetMatching(false);
    }

    private void Update()
    {
        if (_matchTimer >= 0f)
        {
            _matchTimer += Time.deltaTime;
            matchStatusText.text = $"匹配中… {Mathf.FloorToInt(_matchTimer)}s";
        }
    }

    private void SetMatching(bool matching)
    {
        matchButton.gameObject.SetActive(!matching);
        cancelButton.gameObject.SetActive(matching);
        if (!matching) matchStatusText.text = "";
    }

    private void OnDestroy()
    {
        var d = NetworkClient.I?.Dispatcher;
        if (d == null) return;
        d.Off(MsgId.ProfileResp); d.Off(MsgId.MatchResp); d.Off(MsgId.MatchCancelOK);
        d.Off(MsgId.MatchFound); d.Off(MsgId.FriendAddOK); d.Off(MsgId.FriendListOK);
    }
}
```

- [ ] **Step 3: Battle 占位场景**（`Assets/Scenes/Battle.unity` + `BattleSceneStub.cs`：TMP 居中显示 `进入房间 {BattleContext.RoomId}，对手 {OpponentUid}(ELO {OpponentElo})`——计划 4 将替换为真实战斗场景）

```csharp
using TMPro;
using UnityEngine;

public class BattleSceneStub : MonoBehaviour
{
    [SerializeField] private TMP_Text infoText;
    private void Start()
    {
        infoText.text = $"进入房间 {BattleContext.RoomId}\n对手 UID {BattleContext.OpponentUid} (ELO {BattleContext.OpponentElo})\n\n战斗场景（计划4实现）";
    }
}
```

- [ ] **Step 4: 手动冒烟验收（双开）**
  - Editor Play + 一个 Windows Build（或 ParrelSync/两台机），两个客户端各自注册登录
  - 双方点击匹配 → 均在 1 秒内跳转 Battle 占位场景，且 RoomID 相同
  - 单方取消匹配 → 按钮恢复、服务端池清空
  - 互加好友 → 列表显示在线 ●；一端关闭进程 → 另一端刷新（重进大厅）显示 ○

- [ ] **Step 5: 提交**

```bash
git -C "E:\unity_xiangmu\SignalDrift" add Assets ProjectSettings
git -C "E:\unity_xiangmu\SignalDrift" commit -m "feat(ui): 大厅场景——匹配/档案/好友与双端联调通过"
```

---

## Self-Review 结果

1. **规格覆盖**（客户端大厅侧）：连接/心跳(T4)、注册登录(T5)、匹配与取消(T6)、好友与在线状态(T6)、档案展示(T6)、匹配成功进房占位(T6)。战斗输入采集/插值渲染/涂色贴图/结算面板按用户指定顺序归计划 4；断线重连 UI（Token 重入房间）依赖房间服务，也在计划 4。
2. **占位符扫描**：无 TBD；场景搭建步骤为 Editor 手工操作清单（Unity 场景无法用代码块表达，已给出精确控件层级与引用关系）。
3. **类型一致性**：MsgId 数值与计划 2 的 Go 常量逐一核对一致；DTO 字段名=Go JSON tag（下划线命名决策已注明原因）；`Json.Ser/De` 全局唯一序列化入口。
