<p align="center">
  <h1 align="center">沐沐 MumuBot</h1>
  <p align="center">一个基于 ReAct Agent 的赛博 QQ 群友</p>
  <p align="center"><i>Powered by <a href="https://github.com/cloudwego/eino">Eino</a></i></p>
</p>

![MumuBot](https://socialify.git.ci/SugarMGP/MumuBot/image?font=Inter&forks=1&issues=1&language=1&name=1&owner=1&pattern=Circuit+Board&pulls=1&stargazers=1&theme=Auto)

---

> [!WARNING]
> - 项目处于活跃开发阶段，配置和接口可能随时变化
> - QQ 机器人存在封号风控风险，请谨慎使用
> - AI 模型运行会消耗 Token，请注意用量

## ✨ 特性

- 🧠 **ReAct 智能体** — 通过观察-思考-行动循环自主决策行动
- 💬 **拟人对话** — 可自定义人格、语言风格、兴趣话题，说话像真人群友
- 🧩 **丰富工具集** — 发言、沉默、戳一戳、贴表情、发表情包、查群公告、网页浏览等 20+ 内置工具
- 📝 **长期记忆** — MySQL + Milvus 向量数据库，支持语义检索相关记忆
- 👤 **群友画像** — 自动记录群友说话风格、兴趣、活跃度、亲密度
- 🎭 **情绪系统** — 心情、精力、社交意愿三维情绪状态，随对话自然变化
- 👀 **多模态理解** — 支持视觉模型识别图片和视频内容
- 🖼️ **表情包系统** — 自动收集群内表情包，按描述检索并发送
- 📖 **持续学习** — 主动学习群内黑话/梗和独特的表达方式，融入群文化
- ⏰ **时段策略** — 可配置不同时间段的发言活跃度
- 🔌 **MCP 扩展** — 支持通过 MCP 协议接入外部工具，无限扩展能力
- 🖥️ **管理后台** — 快速查看总览、审核学习结果、管理记忆与表情包、浏览群友画像
- 📊 **监控接口** — 提供健康检查与状态接口，方便接入部署和运维流程

## 🚀 快速开始

### 环境要求

| 依赖 | 说明 |
|------|------|
| Go 1.25+ | 编译运行 |
| Node.js 22+ | 构建前端资源 |
| MySQL | 存储记忆、消息日志、群友画像 |
| Milvus（可选） | 向量数据库，启用语义记忆检索 |
| NapCat / go-cqhttp | OneBot 11 协议实现 |
| 大语言模型 API | 兼容 OpenAI 格式 |

### 使用发布包

GitHub Release 提供 Linux、Windows、macOS 的打包产物。归档内包含：

- 可执行文件
- `config/config.yaml` 示例配置
- `config/mcp.json` 示例配置
- `README.md` 和 `LICENSE`

运行发布包时不需要额外部署前端静态文件，管理后台所需资源已经内嵌进二进制。

### 从源码构建

```bash
# 1. 克隆项目
git clone https://github.com/SugarMGP/MumuBot.git
cd MumuBot

# 2. 安装前端依赖并构建后台资源
npm ci
npm run build

# 3. 生成 templ 视图代码
go run github.com/a-h/templ/cmd/templ@latest generate ./internal/web/views

# 4. 编译
go build -o mumu-bot .
```

### 配置与启动

如果你使用的是 GitHub Release 里的打包归档，可直接编辑归档自带的 `config/config.yaml` 和 `config/mcp.json`，不用再执行下面的复制命令。

```bash
# 1. 复制配置文件并按需修改
cp config/config.example.yaml config/config.yaml
cp config/mcp.example.json config/mcp.json

# 2. 编辑配置（填入 LLM API Key、数据库信息等）
# 也可通过环境变量配置敏感信息：
#   MUMU_LLM_API_KEY                    - LLM API Key
#   MUMU_AUX_LLM_API_KEY                - 辅助模型 API Key（用于后台学习任务）
#   MUMU_STYLE_CLASSIFICATION_API_KEY   - 风格分类模型 API Key
#   MUMU_EMBEDDING_API_KEY              - Embedding 模型 API Key
#   MUMU_VISION_API_KEY                 - 视觉模型 API Key
#   MUMU_MYSQL_PASSWORD                 - MySQL 密码

# 3. 启动
./mumu-bot
```

默认情况下服务监听 `8080` 端口，可通过配置里的 `server.port` 修改。

访问端口下 `/admin` 目录来进入管理后台，使用 `web.admin_key` 登录；如果该配置为空，后台会保持关闭状态。

## 🔧 MCP 工具扩展

通过编辑 `config/mcp.json` 接入外部 MCP 服务器，支持 SSE 和 Stdio 两种传输方式：

```json
{
    "servers": [
        {
            "name": "example-mcp-server-sse",
            "enabled": false,
            "type": "sse",
            "url": "http://localhost:3333/sse",
            "tool_name_list": [],
            "custom_headers": {
                "Authorization": "Bearer YOUR_TOKEN_HERE"
            }
        },
        {
            "name": "example-mcp-server-stdio",
            "enabled": false,
            "type": "stdio",
            "command": "",
            "args": [],
            "env": []
        }
    ]
}
```

## 🤝 贡献

**欢迎任何形式的贡献！** 无论是提交 Bug 报告、功能建议，还是直接提交代码，我们都非常感谢。

<a href="https://github.com/your-username/MumuBot/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=SugarMGP/MumuBot" />
</a>

## ❤️ 致谢

- **[cloudwego/eino](https://github.com/cloudwego/eino)** — 字节跳动开源 AI Agent 框架
- **[NapNeko/NapCatQQ](https://github.com/NapNeko/NapCatQQ)** — 现代化 OneBot 协议实现
- **[Mai-with-u/MaiBot](https://github.com/Mai-with-u/MaiBot)** — 灵感来源和设计参考
