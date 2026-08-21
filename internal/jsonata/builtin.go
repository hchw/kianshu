package jsonata

import "strings"

// JSONata 语言内置函数的单一来源（xiatechs/jsonata-go v1.8.8 baseEnv）。
// 两个消费者：
//   - BuiltinFuncNames：flow 包静态检查（未知函数 warning）的可用函数集合；
//   - BuiltinFuncDocs：service 系统提示词注入（让 Agent 知道语言标准库
//     能做什么、怎么签名），避免 LLM 凭预训练记忆猜函数名。
//
// 防漂移测试 TestBuiltinFuncNamesCallable 逐名探测求值，库升级改名/删名
// 即失败。

// builtinFuncNamesRaw 是全量清单（71 个，不含 $ 前缀），按库 baseEnv 提取。
const builtinFuncNamesRaw = `abs accumulatingSlice append average base64decode base64encode
boolean ceil contains count dateTimeDim dateTimeDimLite decodeUrl decodeUrlComponent
distinct each encodeUrl encodeUrlComponent error eval exists filter floor formatBase
formatNumber fromMillis hash256 hashmd5 join keys length lookup lowercase map match
max merge min not number objectsToDocument objmerge oneToManyJoin pad power random
reduce renameKeys replace reverse round shuffle sift single sjoin sort split spread
sqrt string substring substringAfter substringBefore sum timeSince toMillis trim
type unescape uppercase zip`

// BuiltinFuncNames 返回 JSONata 内置函数全名清单（不含 $ 前缀）。
func BuiltinFuncNames() []string {
	return strings.Fields(builtinFuncNamesRaw)
}

// builtinFuncDocs 是系统提示词注入用的分组速查文档（常用子集 + 完整清单指引）。
// 函数签名以 JSONata 语言标准为准（xiatechs 实现的行为与标准一致）。
const builtinFuncDocs = `【JSONata 内置函数库（语言标准库，按族分组）】
以下是 JSONata 语言自带的常用函数（无需 import，直接用 $ 前缀调用）。除下列
常用函数外，运行环境还提供完整的 71 个内置函数；拼写函数名前尽量先对照清单，
拼错的名字会触发静态检查 warning。
- 数值：$number(v) 转数字；$abs(v)；$ceil(v)/$floor(v)/$round(v[, 精度])；$sqrt(v)；$power(a, b)；$sum(arr)；$min(arr)/$max(arr)；$average(arr)；$formatNumber(v, 模式[, 选项])；$formatBase(v[, 进制])
- 字符串：$string(v) 转字符串；$length(s)；$uppercase(s)/$lowercase(s)；$trim(s)；$substring(s, 起[, 长])；$substringBefore(s, 子串)/$substringAfter(s, 子串)；$split(s, 分隔[, 限])；$join(arr[, 分隔])；$replace(s, 模式, 替换[, 限])；$contains(s, 子串)；$match(s, 模式[, 旗标])；$pad(s, 宽[, 填充])
- 数组：$map(arr, function($v){ ... }) 逐元素映射；$filter(arr, function($v){ 条件 }) 过滤；$reduce(arr, function($acc, $v){ ... }[, 初值]) 折叠；$sort(arr[, function($a, $b)]) 排序；$reverse(arr)；$distinct(arr) 去重；$count(arr)；$append(a1, a2)；$zip(...)；$single(arr) 恰一元素；$shuffle(arr)
- 对象：$keys(obj) 键名数组；$lookup(obj, 键) 取属性；$merge(obj 数组) 合并；$spread(obj) 展开为 [键, 值] 对；$each(obj, function($v, $k){ ... })
- 逻辑/其它：$boolean(v)；$not(v)；$exists(v)；$type(v)；$error(消息)；$eval(表达式)；$random()；$base64encode(s)/$base64decode(s)；$encodeUrl(s)/$encodeUrlComponent(s)/$decodeUrl(s)/$decodeUrlComponent(s)
注意：JSONata 内置函数名大小写敏感（$uppercase 可调用、$UPPERCASE 会报错）；
lambda 用 function($v){ ... } 写法（本环境不支持 => 箭头语法）。

【表达式求值作用域（重要，易错）】
表达式（adapter 的 expr、cache-set 写入表达式）只在节点的输入数据上求值，
变量绑定规则固定：
- $cache 不是可求值变量，不能在表达式里直接写 $cache.xxx（会得到 undefined）。
  取共享缓存的值必须 1) 在节点 inputs 里用 source: "$cache.xxx" 声明该输入键，
  2) 然后在表达式里按不带前缀的键名引用（如 s_method、ts）。
- cache-set 写入表达式中额外绑定 $static：字符串常量放 config.static 里，
  表达式用 $static.<key> 引用（不要直接内联字符串字面量）。
- 其余数据一律按当前节点的输入键名（祖先输出/已解析缓存/循环上下文）引用。
规律：$cache.xxx 只出现在 inputs[].source 做取值；表达式内引用解析后的键名或 $static。`

// BuiltinFuncDocs 返回内置函数速查文档（系统提示词注入用）。
func BuiltinFuncDocs() string {
	return builtinFuncDocs
}