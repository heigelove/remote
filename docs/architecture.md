# Remoto 技术架构设计方案

Windows 远程桌面应用：Go 服务端 + MySQL + 可登录客户端（设备列表 / 远程控制）+ 美观桌面 UI + 简易管理后台（React）。

---

## 总体架构

系统拆成 **4 个角色**：

| 角色 | 职责 | 建议技术 |
|------|------|----------|
| **API / 信令服务** | 登录鉴权、设备管理、会话协调、中继兜底、管理 API | Go |
| **数据库** | 用户、设备、会话、审计日志 | MySQL 8 |
| **桌面 Client** | 登录、设备列表、远程窗口；本机被控（采集 / 键鼠） | Wails v2（Go + React） |
| **管理后台 Admin** | 查看/管理用户、设备、操作与审计日志 | React + TypeScript（Web） |

```
┌─────────────┐                    ┌──────────────────┐
│  Client     │◄── HTTPS/WSS ─────►│                  │
│  (Wails)    │                    │  Go API Server   │◄──► MySQL
└──────┬──────┘                    │  + Signaling     │◄──► Redis(可选)
       │                           │  + TURN/Relay    │
       │                           │  + Admin API     │
┌──────┴──────┐                    └────────┬─────────┘
│  Admin Web  │◄── HTTPS ───────────────────┘
│  (React)    │         /api/v1/admin/*
└─────────────┘
       │
       │ WebRTC (优先) / Relay (兜底)        │ WSS 长连接
       ▼                                    ▼
┌─────────────────────────────────────────────┐
│              Agent (被控 Windows)            │
│  屏幕采集 → 编码 → 传输 ← 键鼠/剪贴板注入     │
└─────────────────────────────────────────────┘
```

---

## 1. 服务端（Go）

### 模块划分

```
server/
├── cmd/api/           # HTTP API 入口（含 Admin API）
├── cmd/relay/         # 可选：独立中继（流量大时拆分）
├── internal/
│   ├── auth/          # JWT / Session（普通用户 + 管理员）
│   ├── user/          # 用户 CRUD
│   ├── device/        # 设备注册、心跳、归属
│   ├── session/       # 远程会话生命周期
│   ├── signaling/     # WebRTC SDP/ICE 信令（WebSocket）
│   ├── relay/         # P2P 失败时的流量中继
│   ├── admin/         # 管理后台业务（用户/设备/日志查询）
│   ├── audit/         # 审计与操作日志写入
│   └── middleware/    # 鉴权、角色校验、限流、日志
└── pkg/
```

### 推荐技术栈

- **Web 框架**：Gin 或 Fiber
- **WebSocket**：gorilla/websocket 或 nhooyr.io/websocket
- **ORM**：GORM
- **配置**：Viper
- **日志**：zap / slog（同时落库到 `audit_logs` / `operation_logs`）
- **鉴权**：JWT（Access + Refresh）；Admin 使用独立角色声明（`role=admin`）
- **密码**：bcrypt / argon2

### 核心 API（示意）— 客户端 / Agent

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/login` | 登录，返回 JWT |
| POST | `/api/v1/auth/refresh` | 刷新 Token |
| GET | `/api/v1/devices` | 当前用户设备列表 |
| POST | `/api/v1/devices/register` | Agent 注册（设备 Token） |
| PUT | `/api/v1/devices/:id/heartbeat` | 在线心跳 |
| POST | `/api/v1/sessions` | 发起远程会话 |
| WS | `/ws/signaling` | SDP/ICE 信令 |
| WS | `/ws/agent` | Agent 长连接（状态、命令） |

### 管理后台 API（示意）— 需 Admin 角色

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/admin/auth/login` | 管理员登录 |
| GET | `/api/v1/admin/users` | 用户列表（分页、搜索） |
| GET | `/api/v1/admin/users/:id` | 用户详情（含名下设备数） |
| PUT | `/api/v1/admin/users/:id/status` | 启用 / 禁用用户 |
| GET | `/api/v1/admin/devices` | 全量设备列表（分页、按用户/在线筛选） |
| GET | `/api/v1/admin/devices/:id` | 设备详情 |
| DELETE | `/api/v1/admin/devices/:id` | 解绑 / 删除设备 |
| GET | `/api/v1/admin/logs` | 审计 / 操作日志（分页、按类型/时间筛选） |
| GET | `/api/v1/admin/stats` | 简易概览：用户数、在线设备、今日会话 |

所有 `/api/v1/admin/*` 经 `AdminAuth` 中间件：校验 JWT 且 `role == admin`。

### 设备在线模型

Agent 启动后用 **设备凭证** 连 `/ws/agent`，定期心跳；服务端把状态写成 `online/offline`，Client 与 Admin 均可看到「在线 / 离线」。

---

## 2. MySQL 数据模型

```sql
-- 用户（普通用户与管理员共用表，用 role 区分）
users (
  id BIGINT PK,
  username VARCHAR(64) UNIQUE,
  password_hash VARCHAR(255),
  display_name VARCHAR(128),
  role VARCHAR(16),        -- user | admin
  status TINYINT,          -- 1正常 0禁用
  created_at, updated_at
)

-- 设备（归属用户）
devices (
  id BIGINT PK,
  user_id BIGINT FK,
  device_uid VARCHAR(64) UNIQUE,  -- Agent 机器唯一 ID
  name VARCHAR(128),              -- 用户可改显示名
  os_version VARCHAR(64),
  agent_version VARCHAR(32),
  last_ip VARCHAR(45),
  last_seen_at DATETIME,
  status TINYINT,                 -- 在线状态可由 Redis 更实时，DB 作兜底
  auth_token_hash VARCHAR(255),   -- 设备凭证哈希
  created_at, updated_at
)

-- 远程会话记录
remote_sessions (
  id BIGINT PK,
  user_id BIGINT,
  device_id BIGINT,
  started_at, ended_at,
  quality VARCHAR(16),            -- 720p/1080p 等
  bytes_relayed BIGINT,
  end_reason VARCHAR(64)
)

-- 审计 / 操作日志（管理后台「日志」页数据源）
audit_logs (
  id BIGINT PK,
  actor_type VARCHAR(16),         -- user | admin | agent | system
  actor_id BIGINT NULL,           -- 对应用户/管理员 ID，系统可为 NULL
  action VARCHAR(64),             -- login / logout / device.register / session.start / admin.disable_user ...
  resource_type VARCHAR(32),      -- user | device | session | ...
  resource_id BIGINT NULL,
  ip VARCHAR(45),
  user_agent VARCHAR(255),
  detail JSON,                    -- 补充上下文（勿存敏感明文）
  created_at DATETIME INDEX
)
```

设备列表（Client）只查 `WHERE user_id = ?`；Admin 可查全表并按用户过滤。高并发在线状态可再加 **Redis**（`device:{id}:online`），MySQL 仍作权威归属与审计。

---

## 3. 远程桌面传输方案（重点）

推荐 **WebRTC 为主 + 服务端中继兜底**：

1. Client 请求建立会话 → 服务端校验「用户拥有该设备」且 Agent 在线
2. 双方通过信令交换 SDP / ICE
3. 能直连则 P2P（延迟低、省服务器带宽）
4. NAT 打洞失败走 TURN / 自建 Relay

### Agent 侧能力（Windows）

| 能力 | 实现思路 |
|------|----------|
| 屏幕采集 | DXGI Desktop Duplication（首选），失败回退 GDI |
| 视频编码 | H.264（硬件优先：NVENC/QSV/AMF），软编 x264 |
| 音频（可选） | WASAPI loopback |
| 键鼠注入 | `SendInput` / `mouse_event` |
| 剪贴板同步 | Win32 Clipboard API |
| 文件传输（二期） | 独立可靠通道 |

可用 **pion/webrtc**（Go）在 Agent 与 Client 间建媒体/数据通道；控制消息（键鼠坐标、分辨率、质量）走 DataChannel。

---

## 4. 客户端界面（控制端）

### 为何选 Wails

- 后端与 Agent 同为 Go，技术栈统一
- UI 用 **React + TypeScript**，易做现代桌面感
- 打包成单个 `.exe`，无 Electron 那么重

备选：Tauri + React、Flutter Desktop、WinUI 3。

### 界面信息架构

```
登录页
  └─ 主窗口（侧栏 + 内容）
        ├─ 设备列表（卡片/表格，在线状态、系统信息）
        ├─ 远程桌面会话窗（全屏/窗口，工具条）
        ├─ 设置（画质、快捷键、中继偏好）
        └─ 账户（改密、登出）
```

### 视觉方向（避免「AI 模板感」）

- **深色工作台**：近黑底 `#0F1115` + 冷灰字，主色用 **青绿/电青**（如 `#2DD4BF`）做在线与 CTA，不要紫渐变
- **字体**：`IBM Plex Sans` 或 `Geist`（界面）+ `JetBrains Mono`（设备 ID/IP）
- **背景**：极弱网格或径向暗光，不要纯平一块黑
- **设备列表**：大卡片 + 状态点（绿/灰）+ 一键「连接」；少用花哨徽章
- **远程会话**：沉浸式画布，底部半透明工具条（画质、全屏、剪贴板、断开）
- **动效**：登录淡入、列表项 stagger、连接中的骨架/呼吸动画（2～3 处即可）

### 关键页草图

**登录**：居中品牌名「Remoto」+ 短副标题 + 用户名/密码 + 主按钮；左侧或全幅暗色氛围，无多余统计块。

**设备页**：顶栏「我的设备」+ 刷新；网格卡片：`电脑名`、`Win 版本`、`最后在线`、主按钮「远程连接」。

**会话页**：几乎全屏画面；悬停显示底部工具条。

---

## 5. 管理后台（Admin Web · React）

定位：**简单运维后台**，浏览器访问，不远程控设备，只看管用户 / 设备 / 日志。

### 技术选型

| 项 | 选型 |
|----|------|
| 框架 | **React 18+ + TypeScript** |
| 构建 | Vite |
| 路由 | React Router |
| 请求 | fetch / axios（带 Admin JWT） |
| 样式 | 与 Client 同一套 CSS 变量（深色 + 电青强调色），可用 Tailwind |
| 部署 | 静态资源由 Nginx 托管，或由 Go 嵌入 `embed.FS` 同域提供 |

与控制端共用视觉语言，降低维护成本；Admin 走 Web，不打包桌面。

### 界面信息架构

```
管理员登录
  └─ 后台布局（顶栏品牌 + 侧栏）
        ├─ 概览 Dashboard（用户总数 / 在线设备 / 今日会话）
        ├─ 用户管理（表格：搜索、启用/禁用）
        ├─ 设备管理（表格：归属用户、在线状态、解绑）
        └─ 日志（表格：时间、操作者、动作、资源、IP；可筛选）
```

### 页面要点

**登录**：独立 `/admin/login`，仅 `role=admin` 可进；普通用户 Token 访问 Admin API 一律 403。

**用户**：分页表格 — 用户名、显示名、状态、设备数、注册时间；行内「禁用 / 启用」。首版不做复杂权限编辑。

**设备**：分页表格 — 设备名、UID、归属用户、OS、在线、最后心跳、IP；支持按用户 / 在线筛选；支持解绑删除。

**日志**：时间倒序；筛选项：动作类型、时间范围、操作者；`detail` 可点开 JSON 只读查看。重点记录：登录失败、禁用用户、设备注册/删除、会话起止。

### 视觉

与 Client 一致的深色工作台；侧栏窄、表格为主、少卡片装饰。状态用色点区分（在线青绿 / 离线灰 / 禁用红）。

### 首版刻意做「简单」

- 不做图表大屏、不做实时大屏推送（概览数字定时刷新即可）
- 不做多级 RBAC（仅 `admin` / `user`）
- 不做在线客服式远程协助入口（远程仍只走 Client）

---

## 6. 安全设计（必须做）

- 密码只存哈希；API 全 HTTPS
- JWT 短过期 + Refresh；设备用独立 `device_token`，可吊销
- 会话前校验：`device.user_id == current_user.id`
- 信令/中继鉴权：连接时带 Token，房间号不可猜测
- **Admin 与普通用户 Token 分离校验**（claims 含 `role`）；Admin 登录失败写审计日志
- 操作审计：登录、禁用用户、删设备、开会话等写入 `audit_logs`
- 可选：二次确认（被控端弹「允许远程」）
- Agent 最小权限运行；安装时生成设备绑定码，由用户在 Client 侧绑定到账号
- Admin 后台建议限制来源 IP 或额外放在内网 / VPN 后

---

## 7. 推荐目录结构（单体仓库）

```
remoto/
├── server/                 # Go API + 信令 + Relay + Admin API
├── agent/                  # Windows 被控端
├── client/                 # Wails 控制端
│   ├── frontend/           # React UI
│   └── ...
├── admin/                  # 管理后台（React + Vite）
│   ├── src/
│   │   ├── pages/          # Login / Users / Devices / Logs / Dashboard
│   │   ├── components/
│   │   ├── api/
│   │   └── styles/
│   └── package.json
├── proto/ 或 api/openapi.yaml
├── deploy/                 # docker-compose: api, mysql, redis, coturn, nginx
└── docs/
    └── architecture.md
```

本地可用 `docker-compose` 起 MySQL + Redis + coturn（TURN）；Nginx 将 `/admin` 指到静态资源，`/api`、`/ws` 反代到 Go。

---

## 8. 分阶段落地（建议）

| 阶段 | 目标 |
|------|------|
| **MVP** | 用户登录、设备注册/列表/心跳；Admin：登录 + 用户/设备/日志三页只读为主（禁用用户、解绑设备可写）；WebSocket 信令；GDI 截屏 + JPEG；键鼠；中继模式 |
| **V1** | DXGI + H.264、WebRTC P2P、画质切换、剪贴板、会话记录；Admin 筛选与概览统计 |
| **V2** | 文件传输、多显示器、权限确认、自动更新、安装包；Admin 导出日志、IP 白名单 |

MVP 用「截帧 + WebSocket/自定义 TCP 中继」也能先跑通产品闭环，再换成 WebRTC，降低首版风险。

---

## 9. 技术选型一览

| 层 | 选型 |
|----|------|
| API | Go + Gin + GORM + JWT（user / admin 角色） |
| DB | MySQL 8（+ Redis 可选） |
| 实时 | WebSocket 信令 + WebRTC（pion）/ 自建 Relay |
| Client | Wails + React + TS + Tailwind（或自写 CSS 变量） |
| **Admin** | **React + TS + Vite + React Router**（Web 静态站点） |
| Agent | Go + DXGI/GDI + SendInput |
| 部署 | Docker + Nginx(TLS) + coturn |

---

## 后续可补充

- 完整 MySQL DDL（含 `audit_logs` 索引）
- 信令时序图（会话建立 / ICE / 中继切换）
- Wails 客户端与 Admin 共用配色 CSS 变量
- Admin 页面组件树与路由表
