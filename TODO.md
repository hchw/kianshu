
## E4 阶段性(分阶段)验证策略 — 待实现

**日期:** 2026-08-16
**变更:** phased-validation
**状态:** 待实现

### 问题/动机

当前 `internal/flow/validate.go` 的 `Validate` 把整树校验拆成若干独立检查
(树形 → I/O 契约 → adapter 语法 → try/catch → loop),但 `agent_tools.go`
的 `execValidateFlow` 一次性返回全部 errors/warnings,LLM 难以按"阶段"
渐进修复。尤其 E3 新增大量加密/签名扩展后,adapter 表达式若引用了
未注册的函数,只会在运行时才报 lookup 错误,校验期无法提前拦截。

### 计划

- 在 `validateAdapters`(或新增 `validateAdapterExtensions`)阶段,静态扫描
  adapter 表达式引用的 `$xxx()` 函数名,逐一比对 `jsonata.Exts()` 注册表,
  未注册者报 `adapter.ext_unknown`(error,带 available 列表)。
- 把校验结果按"阶段"分组输出(树形 / 契约 / 适配器 / 异常 / 循环),
  让 agent 的 `expected_format` 能指明"当前该修哪一类"。
- 考虑在生成循环中让 LLM 在关键里程碑(建完认证链、建完业务序列)
  主动调用一次 `validate_flow`,而非只在结尾校验一次(阶段性验证策略)。

### 待查

- JSONata 表达式能否在不编译求值的前提下提取被调用的函数名(正则 / AST)
- 分阶段校验的"阶段"边界是否要与前端 NodePanel 的红点提示对齐


## E3 适配器 JSONata 加密/签名/时间戳扩展 — 待实现

**日期:** 2026-08-16
**变更:** adapter-crypto-extensions
**状态:** 待实现

### 问题/动机

复杂第三方 API(支付、云厂商、开放平台)的鉴权常要求客户端自行构造签名,
而非简单的 Bearer / API-Key 头。当前 JSONata 扩展(`internal/jsonata/jsonata.go`
的 `Exts()`)只有基础工具:base64、md5/sha1/sha256(hex)、hmac(hex),
不足以支撑以下常见签名套路:

- 参数按 key 排序后用 HMAC-SHA256 再 base64(阿里云 / 腾讯云 / 微信支付)
- RSA 私钥签名(RSA-SHA256)与验签(SHA1WithRSA / SHA256WithRSA)
- AES/DES/3DES 对报文或字段做对称加解密
- 签名串需要时间戳、nonce、hex/base64 编解码

若没有对应扩展,LLM 在 adapter 节点里只能把签名逻辑外置到代码,
破坏"一棵自描述树"的设计初衷。

### 计划新增的扩展函数(分组)

**时间戳 / 随机数(签名拼装必备)**
- `$now()` unix 秒;`$nowMs()` 毫秒;`$nowISO()` ISO-8601 字符串
- 可选 `$uuid()`(签名 nonce 用)

**编码 / 解码**
- `$hexencode(s)` / `$hexdecode(s)`(已有 base64)
- `$urlencode(s)` / `$urldecode(s)`

**哈希与 MAC(补齐 raw 字节输出)**
- 已有 md5/sha1/sha256(hex) → 新增 md5b64/sha1b64/sha256b64(返回 base64 原始字节)
- 已有 hmac(hex) → 新增 hmacb64(secret, msg, algo) 返回 base64 原始字节

**对称加密套件(输入/输出均为 base64,key/iv 支持 base64/hex/utf8 派生)**
- `$aesEncrypt(plain, key, iv, mode)` / `$aesDecrypt(cipher, key, iv, mode)`(mode: cbc|ecb|ctr)
- `$desEncrypt` / `$desDecrypt`(DES)
- `$des3Encrypt` / `$des3Decrypt`(3DES / TripleDES)

**非对称签名套件**
- `$rsaEncrypt(plain, pubPEM)` / `$rsaDecrypt(cipher, privPEM)`(OAEP)
- `$rsaSign(msg, privPEM, algo)`(algo: sha1|sha256,返回 base64 签名)
- `$rsaVerify(msg, sig, pubPEM, algo)` → bool

### 理想方向

在 `internal/jsonata/jsonata.go` 的 `Exts()` 中统一注册;
`Eval`/`EvalWithVars` 已通过 `registerExts` 自动挂载,执行器
(`exec.go` 的 adapter 求值点)与校验器(`validate.go` 的 adapter 校验)无需改动。
配套在 `agent.go` 的系统提示里补一段"复杂签名"范例,教 LLM 用 adapter 节点
拼装 `timestamp + nonce + sort(params) + hmac-sha256 + base64` 等套路。

### 待查

- xiatechs/jsonata-go 扩展函数的可选参数与类型转换规则(字符串/数字/[]byte)
- 对称加密 key/iv 的长度归一策略(PKCS7 填充、非 16/24/32 字节如何处理)
- 是否需要把"签名拼装"做成独立节点类型,还是仅依赖 adapter + 扩展
- 前端 `web/src/lib/tree.ts` 是否要镜像这些扩展(纯校验用途,可能不需要)


## E2 执行器硬编码 auth 头构造 — 待重构

**日期:** 2026-08-09  
**变更:** agent-cache-set-guidance  
**状态:** 临时方案已验证可用,待后续清理

### 问题

`internal/exec/exec.go` 的 `apiOutput` 中,`isAuthKey` + `execSecurityScheme` 在
执行器层硬编码了 Swagger security scheme → HTTP header 的映射:
- `BearerAuth` → `Authorization: Bearer <token>`
- `apikey` → `X-API-Key: <key>`

这是执行器不该有的职责:
1. 执行器耦合了 API 契约概念(security scheme)
2. 树不够自描述——看 `auth ← $cache.token` 看不出最终 HTTP 头
3. `isAuthKey` 是魔法键名规则,和废掉的"同级排序靠左"同类问题

### 理想方向

让 LLM 通过 `config.headers` 显式准备 auth 头,执行器不做任何特殊处理:
```
config.headers: {"Authorization": "Bearer {{jsonata引用token}}"}
```
类比 flow 1 的 `{"Authorization": "Bearer {{c.auth_token}}"}`。

去掉: `isAuthKey`(三处) + `execSecurityScheme` + `Security` 字段。

### 待查

- `config.headers` 在 `apiOutput` / `HTTPCallAPI` 中的处理路径
- 是否需要 adapter 节点构造 `"Bearer " & token` 再传入 headers
- validate 层如何适配

