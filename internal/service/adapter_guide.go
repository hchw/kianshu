package service

import "github/hchw/kianshu/internal/jsonata"

// adapterExtGuide 是"adapter 节点 JSONata 扩展能力"全局指引章节（D6）：
// 按族分组的能力清单、使用时机判别、经典用法与易错点。
// 边界：本章节不承载任何特定厂商的签名配方模板——canonical string
// 拼装顺序、密钥语义等业务规则由用户经流级上下文文档（flow-context-doc）
// 提供，优先级高于本全局指引。
const adapterExtGuide = `【adapter 节点 JSONata 扩展能力】
adapter 节点的 expr 与 cache-set 写入表达式可直接调用以下扩展函数
（全部以 $ 前缀调用，输入输出均为字符串；无需 import）：

一、能力清单（按族分组）
- 时间戳/随机：$now() -> Unix 秒；$nowMs() -> Unix 毫秒；$nowISO() -> RFC3339 UTC 时间串；$uuid() -> UUID v4
- 编码：$hexencode(str) -> 十六进制；$hexdecode(hex) -> 原串；$urlencode(str) -> RFC3986 百分号编码；$urldecode(str) -> 原串；$base64encode(str)/$base64decode(str)
- 排序：$sortKeys(obj) -> 按键名字典序（码点序/字节序：大写字母先于小写）返回键名数组——签名场景 canonical 参数排序的专用入口；内置 $sort(数组[, 比较函数]) 可排任意数组
- 哈希/MAC（原始字节版，输出 base64）：$md5b64(s)、$sha1b64(s)、$sha256b64(s)；$hmacb64(secret, msg, algo)（algo 仅 sha1/sha256/md5）
  对应十六进制版（输出 hex 文本）：$md5/$sha1/$sha256/$hmac
- 对称加密：$aesEncrypt(plain, key, iv, mode) -> base64 密文；$aesDecrypt(cipherB64, key, iv, mode) -> 明文；mode 仅 cbc/ecb/ctr；key/iv 支持 base64:/hex: 前缀或裸字符串（详见易错点）；填充 PKCS7
- 非对称：$rsaEncrypt(plain, pubPEM) / $rsaDecrypt(cipherB64, privPEM)（OAEP）；$rsaSign(msg, privPEM, algo) -> base64 签名；$rsaVerify(msg, sigB64, pubPEM, algo) -> boolean（algo 仅 sha1/sha256）

二、什么时候用（场景判别）
- 接口要求签名/认证头（HMAC、RSA 签名、时间戳+nonce）：先看用户流级文档里的签名规则
- 报文加解密（接口交互密文、加解密字段）：AES 对称加密；密钥交换场景用 RSA
- 参数规范化（URL 编码、hex 编码、生成唯一 ID）
- 响应验签/解密（把响应数据接入后续断言）

三、如何用（经典套路）
- 签名串拼装：把规范化参数拼接为签名串，再对签名串调用哈希族
  先按键排序再拼 k=v&：$join($map($sortKeys(参数对象), function($f){ $f & "=" & $string($lookup(参数对象, $f)) }), "&")
  再对拼好的串做 MAC：$hmacb64($urldecode($hexdecode('...')), 上一步的串, 'sha256')，把结果放进 cache-set 供 api 节点作为请求头来源
- 时间敏感参数：$nowISO()/$nowMs() 直接拼进参数或签名串
- 加密载荷：$aesEncrypt('原始载荷', key, iv, 'cbc')，密文结果按 base64 传输
- 多步签名依赖：用 cache-set 暂存中间结果，签名表达式里引用 $cache.xxx

四、易错点（严格遵守）
- 十六进制版与原始字节版不同：$hmacb64 输出原始 MAC 字节的 base64；
  不要写成 $base64encode($hmac(...))——那会把 hex 文本再 base64 一次，结果错误
- key/iv 格式：base64:/hex: 前缀表示二进制 key（严格校验长度），裸字符串是口令式 key（自动归一）；
  base64:/hex: 的 key 仅接受 16/24/32 字节（AES-128/192/256）
- AES 的 iv 恒为 16 字节；ecb/cbc 密文长度必须是 16 的倍数（PKCS7）；ctr 无填充限制
- RSA 私钥 PEM 支持 PKCS#1 与 PKCS#8 包装，公钥支持 PKCS#1/PKIX；密钥不匹配或签名损坏会报错，验签失败返回 false
- 未知函数会得到校验 warning——拼写函数名时对照上面清单

注意：特定厂商的 canonical string 模板、签名拼装顺序与密钥获取方式不属于全局规则，
以流级上下文文档（用户提供的业务文档/签名规则）为准。`

// adapterBuiltinGuide 注入 JSONata 语言内置函数库速查（单一来源见
// jsonata.BuiltinFuncDocs），让 Agent 知道语言标准库能做什么、怎么签名。
var adapterBuiltinGuide = jsonata.BuiltinFuncDocs()
