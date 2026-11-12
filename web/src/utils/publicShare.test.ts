import { describe, expect, it } from 'vitest'
import { buildPublicShareURL } from './publicShare'

describe('buildPublicShareURL', () => {
  it('uses the frontend origin for directory browser pages', () => {
    expect(buildPublicShareURL('http://localhost:5173', {
      token: 'abc-123',
      isDir: true,
      name: '资料'
    })).toBe('http://localhost:5173/share/abc-123')
  })

  it('opens files through the frontend API proxy with an encoded filename', () => {
    expect(buildPublicShareURL('http://localhost:5173', {
      token: 'abc-123',
      isDir: false,
      name: '季度 报告.pdf'
    })).toBe('http://localhost:5173/api/v1/public/share/abc-123/%E5%AD%A3%E5%BA%A6%20%E6%8A%A5%E5%91%8A.pdf')
  })
})
