<div align="center">

![鉴枢](web/public/kianshu.png)

# 鉴枢（Kianshu）

**让接口集成测试从“写脚本”变成“描述、生成、验证和持续运行”。**

</div>

---

## 鉴枢解决什么问题

接口数量增长后，集成测试通常会遇到这些问题：

- 测试人员需要反复阅读 Swagger，再手工拼接请求参数、认证信息和上下游数据。
- 一个接口的响应往往是下一个接口的输入，测试数据提取、传递和清理容易散落在脚本里。
- 测试用例能跑起来，但难以让团队成员理解、修改和复用；失败后也不容易定位是哪一步出了问题。
- 接口变更后，旧测试可能依赖最新文档或代码，历史结果无法可靠复现。
- 临时验证、回归测试和定时任务分散在不同工具中，维护成本高。

鉴枢面向需要验证多个接口协作关系的团队，把 OpenAPI/Swagger 文档、自然语言意图和可视化测试流程连接起来，帮助团队更快建立稳定、可追踪的集成测试。

## 怎么用

### 1. 启动服务并注册账号

```bash
# 一键启动后端和前端
make dev
```

然后访问 <http://localhost:5173>，注册并登录。默认使用 SQLite，数据库文件为项目根目录下的 `kianshu.db`。

也可以分别启动：

```bash
make dev-backend   # 后端：http://localhost:8080
make dev-frontend  # 前端：http://localhost:5173
```

### 2. 创建测试集并导入接口文档

创建测试集，配置被测服务的 Host，然后导入 Swagger/OpenAPI 文档。鉴枢会将文档中的接口整理为可检索、可组合的测试单元；同一 `method + path` 的接口会自动去重。

### 3. 配置模型服务

在“模型 Provider”中填写兼容 OpenAI API 的服务地址、模型和 API Key，并先测试连通性。API Key 会被加密保存，不需要写入测试流程。

### 4. 让 Agent 生成测试流

在流编辑器中描述测试目标，例如“先登录，提取 token，再创建订单并校验订单状态”。Agent 会根据已导入的接口生成树形测试流，并自动补充接口之间的数据适配、断言和必要的异常处理。

生成结果不是黑盒脚本：你可以在画布中查看和调整每一步，直接修改参数、表达式、节点关系或断言，也可以继续用自然语言让 Agent 修改。保存前会执行完整校验，避免把不完整的流程投入运行。

### 5. 试运行、保存并持续验证

先执行草稿试运行，查看每个节点的请求、响应和执行状态；确认结果后保存并启用。启用会生成不可变版本，之后可以：

- 查看执行日志和失败节点；
- 重新运行指定历史版本，复现当时的测试逻辑；
- 配置 Cron 定时执行回归测试；
- 与团队成员共享测试集，并区分只读和编辑权限。

## 使用价值

- **降低测试编写门槛**：从接口文档和测试意图出发，减少手工编写脚本的工作量。
- **表达真实业务链路**：通过树形步骤、显式数据传递、循环和 try/catch，覆盖登录、列表遍历、异常分支等场景。
- **让测试可读、可改、可协作**：流程是人可以理解和编辑的执行单元，而不是只能由作者维护的黑盒代码。
- **提升问题定位效率**：每个节点都有独立执行结果，失败影响范围清晰，便于快速找到接口、参数或断言问题。
- **保证结果可追溯**：版本快照与执行日志绑定，历史测试不依赖当前 Swagger 文档，便于回归和审计。
- **统一临时验证与持续回归**：同一套流程既能立即试跑，也能按计划自动执行。

## 适用场景

- 新项目或新版本的接口联调
- 登录、下单、支付、查询等多接口业务链路验证
- Swagger 变更后的回归测试
- 需要列表循环、数据关联或异常分支的复杂接口测试
- 团队共享的接口质量检查和定时巡检

## 开发与检查

```bash
make test      # 运行后端和前端测试
make lint      # Go vet 和前端 lint
make build     # 构建后端与前端
```

API 文档启动后可访问 <http://localhost:8080/swagger/index.html>。

## 环境变量配置

服务通过环境变量配置运行参数。除 `KS_ENC_KEY` 外，其余变量均可使用默认值启动。

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `KS_ADDR` | `:8080` | HTTP 服务监听地址，例如 `:9000` 或 `127.0.0.1:8080` |
| `KS_ENC_KEY` | 无，必填 | Provider API Key 的 AES-GCM 加密密钥，必须为 32 字节；生产环境请使用随机且固定的值 |
| `DB_DRIVER` | `sqlite` | 数据库类型：`sqlite`、`mysql` 或 `postgres` |
| `DB_PATH` | `kianshu.db` | SQLite 数据库文件路径；仅在 `DB_DRIVER=sqlite` 且未设置 `DB_DSN` 时使用 |
| `DB_DSN` | 空 | GORM 数据源连接串；设置后优先使用，适用于 MySQL、Postgres，也可用于 SQLite |
| `KS_SESSION_TTL_HOURS` | `168` | 登录会话有效期，单位为小时，默认 7 天 |
| `KS_EXEC_TIMEOUT` | `60` | 单次 HTTP 请求及整次测试执行的超时时间，单位为秒 |
| `KS_LLM_TIMEOUT` | `600` | 单次 LLM 请求超时时间，单位为秒 |
| `KS_SIGN_SECRET` | 空 | 签名认证使用的 HMAC 密钥；仅在启用签名认证客户端时配置 |

Linux/macOS 示例：

```bash
export KS_ADDR=':9000'
export KS_ENC_KEY='0123456789abcdef0123456789abcdef'
export DB_DRIVER='sqlite'
export DB_PATH='/data/kianshu.db'
./kianshu
```

Windows PowerShell 示例：

```powershell
$env:KS_ADDR = ":9000"
$env:KS_ENC_KEY = "0123456789abcdef0123456789abcdef"
$env:DB_DRIVER = "sqlite"
$env:DB_PATH = "D:\\data\\kianshu.db"
.\\kianshu.exe
```

使用 MySQL 或 Postgres 时，请设置 `DB_DRIVER` 和对应的 `DB_DSN`，并确保数据库已创建且服务进程可以访问。

## Release 发布与打包

项目使用 GitHub Actions 自动构建 Release。向仓库推送符合 `v*` 格式的 Git tag（例如 `v1.0.0`）后，会自动执行前端构建、后端测试和跨平台编译，并创建 GitHub Release。

```bash
git tag v1.0.0
git push origin v1.0.0
```

当前会生成以下发布包：

- `kianshu-linux-amd64.tar.gz`：Linux amd64
- `kianshu-windows-amd64.zip`：Windows amd64

每个发布包包含：

- `kianshu` 或 `kianshu.exe`：后端可执行文件
- `web-dist/`：已构建的前端静态文件
- `README.md`：使用说明

下载对应平台的压缩包并解压后，在包目录中设置必要的环境变量，然后启动程序即可。发布包会优先读取同目录的 `web-dist/`，因此不需要安装 Node.js；SQLite 模式下数据库默认创建为同目录的 `kianshu.db`。

```bash
# Linux 示例；生产环境请替换为随机的 32 字节密钥
export KS_ENC_KEY='0123456789abcdef0123456789abcdef'
./kianshu
```

### Windows PowerShell

下载 `kianshu-windows-amd64.zip` 后解压，在 PowerShell 中进入解压目录。首次运行前设置一个固定的 32 字节 `KS_ENC_KEY`，然后启动程序：

```powershell
Set-Location .\kianshu-windows-amd64
$env:KS_ENC_KEY = "0123456789abcdef0123456789abcdef"
.\kianshu.exe
```

PowerShell 中通过 `$env:` 设置的环境变量只对当前窗口及其启动的进程有效。如果希望永久保存配置，可以使用：

```powershell
[Environment]::SetEnvironmentVariable(
  "KS_ENC_KEY",
  "0123456789abcdef0123456789abcdef",
  "User"
)
```

设置永久环境变量后，请重新打开 PowerShell，再执行：

```powershell
.\kianshu.exe
```

默认启动地址为 <http://localhost:8080>。如果 Windows Defender SmartScreen 拦截未签名程序，请在确认文件来源可信后选择“更多信息”→“仍要运行”。

`KS_ENC_KEY` 用于加密保存 Provider API Key，生产环境必须使用安全且固定的密钥。更多数据库、监听地址和超时配置请参考 `internal/config/config.go`。

## 开源协议

本项目基于 [Apache License 2.0](LICENSE) 开源。

## 联系

- 微信：`hbl826396273`
- 邮箱：[2012hchw@gmail.com](mailto:2012hchw@gmail.com)
