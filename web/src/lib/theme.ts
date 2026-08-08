import { useCallback, useEffect, useState } from 'react'

const THEME_KEY = 'kianshu_theme'

export type Theme = 'light' | 'dark'

/**
 * 明暗主题 hook：读写 localStorage、同步 <html data-theme>、暴露 toggle。
 * 默认深色，与 index.html 内联脚本保持一致。
 */
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => {
    try {
      return localStorage.getItem(THEME_KEY) === 'light' ? 'light' : 'dark'
    } catch {
      return 'dark'
    }
  })

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    try {
      localStorage.setItem(THEME_KEY, theme)
    } catch {
      /* localStorage 不可用时仅保持内存状态 */
    }
  }, [theme])

  const toggle = useCallback(() => {
    setTheme((cur) => (cur === 'dark' ? 'light' : 'dark'))
  }, [])

  return { theme, toggle }
}
