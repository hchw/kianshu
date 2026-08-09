
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

