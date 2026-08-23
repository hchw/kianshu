import { describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import PopConfirm from './PopConfirm'

function setup(extra?: { input?: { defaultValue?: string; placeholder?: string } }) {
  const onConfirm = vi.fn()
  const utils = render(
    <PopConfirm
      message="确定执行?"
      confirmText="确定"
      cancelText="取消"
      onConfirm={onConfirm}
      {...extra}
    >
      <button>触发</button>
    </PopConfirm>,
  )
  fireEvent.click(screen.getByText('触发'))
  return { onConfirm, ...utils }
}

describe('PopConfirm', () => {
  it('renders message and confirms without value when no input', () => {
    const { onConfirm } = setup()
    expect(screen.getByText('确定执行?')).toBeTruthy()
    fireEvent.click(screen.getByText('确定'))
    expect(onConfirm).toHaveBeenCalledWith(undefined)
    cleanup()
  })

  it('renders an input pre-filled with defaultValue when input is provided', () => {
    const { onConfirm } = setup({
      input: { defaultValue: '支付流程 副本', placeholder: '新流名称' },
    })
    const box = screen.getByPlaceholderText('新流名称') as HTMLInputElement
    expect(box.value).toBe('支付流程 副本')
    fireEvent.change(box, { target: { value: '回归基线' } })
    fireEvent.click(screen.getByText('确定'))
    expect(onConfirm).toHaveBeenCalledWith('回归基线')
    cleanup()
  })

  it('submits the current value on Enter', () => {
    const { onConfirm } = setup({ input: { defaultValue: 'A' } })
    const box = document.querySelector('.popconfirm-input') as HTMLInputElement
    expect(box).toBeTruthy()
    fireEvent.change(box, { target: { value: 'B' } })
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(onConfirm).toHaveBeenCalledWith('B')
    cleanup()
  })
})
