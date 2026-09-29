# Remoto

Windows 远程桌面：Go API + SQLite/MySQL、统一桌面客户端（控制端 + 被控端）、React 管理后台。

## 组件

| 目录 | 说明 | 默认地址 |
|------|------|----------|
| `server/` | API / 信令 / 画面中继 | `:8080` |
| `client/` | Wails 桌面端（远程控制 + 本机被控） | — |
| `admin/` | 管理后台（用户 / 设备 / 日志） | `:5173` |

默认账号（首次启动自动种子）：

- 管理员：`admin` / `admin123`
- 演示用户：`demo` / `demo123`

## 快速开始

### 1. 启动 API

```bash
cd server
go run ./cmd/api -config configs/config.yaml
```

### 2. 管理后台

```bash
cd admin
npm install
npm run dev
```

打开 http://127.0.0.1:5173 ，用 `admin` 登录。

### 3. 桌面客户端（控制端 + 被控端）

必须用 Wails 构建（不要裸 `go build`，窗口会打不开）：

```powershell
cd client
# go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails build
.\build\bin\remote-client.exe
```

一个程序里包含：

- **远程控制**：登录、设备列表；连接成功后会新开远程桌面窗口
- **本机被控**：绑定码 / 账号密码绑定、保持在线、开机自启、锁屏输入服务

无界面被控常驻：

```powershell
.\build\bin\remote-client.exe -nogui
.\build\bin\remote-client.exe -bind <绑定码> -nogui
```

## MVP 能力

- 用户登录 / JWT / 设备绑定码注册 / 在线心跳（WebSocket）
- 会话建立 + WebSocket 中继 JPEG 帧 + 键鼠注入
- 控制端连接成功后独立会话窗口
- Admin：概览、用户启用禁用、设备解绑、审计日志

`agent/` 目录为旧版被控端，功能已并入 `client/`。
