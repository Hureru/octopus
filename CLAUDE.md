# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 开发命令

### 后端 (Go)
```bash
go run main.go start                # 启动服务 (默认 0.0.0.0:8080)
go run main.go start --config path  # 指定配置文件
go test ./...                       # 运行所有测试
```

### 前端 (Next.js)
```bash
cd web
pnpm install                        # 安装依赖
pnpm dev                            # 开发服务器 (localhost:3000)
NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:8080" pnpm dev  # 指定后端地址
pnpm build                          # 生产构建 (输出到 web/out/)
pnpm lint                           # ESLint 检查
```

### 完整构建
```bash
cd web && pnpm install && pnpm build && cd ..
mv web/out static/
go run main.go start
```

### 跨平台发布
```bash
./scripts/build.sh build linux x86_64   # 构建指定平台
./scripts/build.sh release              # 构建所有平台
```

### Docker
```bash
docker compose up -d
```

## 架构概览

Octopus 是一个 **LLM API 聚合与负载均衡服务**。Go 后端 (Gin + GORM) 提供 API 代理和管理接口，Next.js 前端提供管理面板。

**启动流程**: `main.go` → `cmd/start.go` → 初始化 Config → DB → Cache → HTTP Server → Background Tasks

**请求流**: Gin Router → Middleware (Auth/CORS/Logger) → Handler → Op (业务逻辑) → DB/Cache

**API 代理流**: Request → Inbound Transformer (协议转换) → Relay → Balancer (负载均衡/熔断) → 外部 LLM API → Outbound Transformer → Response

## 后端关键模块 (`internal/`)

| 模块 | 职责 |
|------|------|
| `conf/` | Viper 配置管理，env 前缀 `OCTOPUS_`，默认读取 `data/config.json` |
| `db/` | GORM 数据库层，支持 SQLite(默认)/MySQL/PostgreSQL，`db/migrate/` 含迁移 |
| `model/` | 数据模型定义 (Channel, Group, User, APIKey, Setting, Stats 等) |
| `op/` | **业务逻辑层 (Service)**，包含内存缓存管理，Handler 调用此层而非直接操作 DB |
| `server/handlers/` | HTTP 请求处理器，按资源分文件 |
| `server/middleware/` | Auth (JWT + API Key)、CORS、Logger、Static 等中间件 |
| `server/router/` | 自定义路由框架，链式注册: `NewGroupRouter(path).Use(mw).AddRoute(route)` |
| `server/auth/` | JWT 生成/验证，API Key 格式 `sk-octopus-*` |
| `server/resp/` | 统一响应格式 `{code, message, data}` |
| `relay/` | API 代理核心，负载均衡策略 (RoundRobin/Random/Failover/Weighted)，熔断器 |
| `transformer/` | 协议转换适配器，`inbound/` 解析请求，`outbound/` 格式化响应，支持 OpenAI/Anthropic/Gemini |
| `task/` | 后台定时任务 (统计持久化、模型同步、价格更新) |
| `client/` | LLM 提供商 HTTP 客户端封装 |
| `utils/log/` | Zap 结构化日志 |
| `utils/cache/` | 分片缓存 (16 shard, xxhash)，运行时内存缓存 + 关机持久化到 DB |

## 前端关键模式 (`web/src/`)

- **状态管理**: Zustand (本地/持久化状态) + TanStack React Query (服务端数据缓存，30s 自动刷新)
- **UI**: shadcn/ui + Radix UI 原语 + TailwindCSS v4 + Framer Motion 动画
- **路由**: 自定义 SPA 路由 (`route/config.tsx` 定义，`ContentLoader` 动态加载)，**不使用** Next.js 文件路由
- **API 层**: `api/client.ts` 基于 fetch 的 HTTP 客户端，`api/endpoints/` 按功能导出 React Query hooks
- **i18n**: next-intl，翻译文件位于 `public/locale/{en,zh_hans,zh_hant}.json`
- **构建**: SSG 静态导出 (`output: "export"`)，嵌入到 Go 二进制的 `static/` 目录

## 配置

运行时配置 `data/config.json`（首次运行自动生成），所有字段可通过 `OCTOPUS_` 前缀环境变量覆盖:
- `OCTOPUS_SERVER_PORT`, `OCTOPUS_SERVER_HOST`
- `OCTOPUS_DATABASE_TYPE` (sqlite/mysql/postgres), `OCTOPUS_DATABASE_PATH`
- `OCTOPUS_LOG_LEVEL`

数据库运行时设置 (CORS 等) 存储在 `Setting` 模型中，通过 `op/setting.go` 缓存访问。

## 贡献规范

- 每个 PR 只包含一个变更主题（一个功能或一个 BUG 修复）
- AI 辅助代码需完成人工审查后提交
