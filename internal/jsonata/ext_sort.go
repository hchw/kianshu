package jsonata

import (
	"sort"

	"github.com/xiatechs/jsonata-go"
)

// extsSort 排序工具族。签名场景高频需求：签名规范（AWS SigV4 / 阿里云 /
// 腾讯云等）普遍要求先把参与签名的键拍成字典序（字节/码点序：大写字母
// 先于小写字母）再拼装 canonical query。内置 $sort 已能排数组，但签名
// 场景通常是"对象按键排序"——$sort($keys(obj)) 两步变一步 $sortKeys(obj)。
//
// 与内置 $sort(数组, 可选比较函数) 的关系：
//   - $sort 接受的是数组 + 可选比较函数，排序策略可由调用方自定义；
//   - $sortKeys 接受的是对象，固定按键名做字典序（字节序）排序，
//     返回键名数组——这正是签名场景唯一需要的排序语义。
func extsSort() map[string]jsonata.Extension {
	return map[string]jsonata.Extension{
		"sortKeys": {
			Func: func(obj map[string]interface{}) []string {
				keys := make([]string, 0, len(obj))
				for k := range obj {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				return keys
			},
		},
	}
}