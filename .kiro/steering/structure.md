# 项目结构

```
awecloud-signaling-server/
├── cmd/                    # 应用入口
│   ├── server/             # Server main.go
│   └── agent/              # Agent main.go
├── internal/               # 内部实现（不可被外部导入）
│   ├── server/             # Server 实现
│   │   ├── api/            # REST API 处理器（Gin）
│   │   ├── grpc/           # gRPC 服务实现
│   │   ├── db/             # 数据库初始化（GORM）
│   │   ├── model/          # 数据库模型
│   │   ├── auth/           # 认证（设备令牌）
│   │   ├── cache/          # 内存缓存
│   │   ├── headscale/      # Headscale 客户端和 ACL 同步
│   │   ├── proxy/          # 反向代理处理器
│   │   └── service/        # 业务逻辑服务
│   ├── agent/              # Agent 实现
│   │   ├── agent.go        # Agent 主逻辑
│   │   ├── tailscale_manager.go
│   │   ├── proxy_manager.go
│   │   └── visitor_manager.go
│   └── common/             # 公共代码
│       ├── config/         # 配置解析（TOML）
│       ├── logger/         # 日志（Logrus）
│       └── banner/         # 启动横幅
├── pkg/                    # 公共包
│   └── proto/              # Protocol Buffers 定义和生成代码
├── web/                    # Vue 3 前端
│   ├── src/
│   │   ├── api/            # API 客户端模块
│   │   ├── components/     # 可复用 Vue 组件
│   │   ├── views/          # 页面组件
│   │   ├── stores/         # Pinia 状态管理
│   │   ├── router/         # Vue Router 配置
│   │   ├── locales/        # 国际化翻译
│   │   ├── types/          # TypeScript 类型
│   │   └── utils/          # 工具函数
│   └── dist/               # 构建输出（由 Server 提供服务）
├── config/                 # 配置示例
├── docs/                   # 设计文档（禁止包含代码）
├── images/                 # 文档用 SVG 图表
├── scripts/                # 构建和运行脚本
├── deployments/            # 部署配置
│   ├── docker/             # Dockerfile
│   └── kubernetes/         # K8s 清单
├── bin/                    # 编译输出（gitignored）
├── data/                   # SQLite 数据库（gitignored）
└── logs/                   # 日志文件（gitignored）
```

## 关键约定

- `internal/` 包是模块私有的
- `pkg/` 包可被外部项目导入
- 数据库模型在 `internal/server/model/`
- API 处理器在 `internal/server/api/`
- 前端 API 客户端与后端结构对应，在 `web/src/api/`
- 设计文档命名规范：`design_<模块>.md`、`design_<模块>_<功能>.md`
