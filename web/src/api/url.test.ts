import { describe, expect, it } from 'vitest'
import { absoluteApiUrl } from './url'

describe('absoluteApiUrl', () => {
  it('uses the browser origin for same-origin API deployments', () => {
    expect(absoluteApiUrl('/v1/import/job-1/stream', '', 'http://localhost:9000')).toBe(
      'http://localhost:9000/v1/import/job-1/stream',
    )
  })

  it('preserves an absolute configured API base path', () => {
    expect(
      absoluteApiUrl(
        '/v1/import/job-1/stream',
        'https://api.example.test/radioledger',
        'https://app.example.test',
      ),
    ).toBe('https://api.example.test/radioledger/v1/import/job-1/stream')
  })

  it('resolves a relative configured API base against the browser origin', () => {
    expect(absoluteApiUrl('/v1/import/job-1/stream', '/api', 'https://app.example.test')).toBe(
      'https://app.example.test/api/v1/import/job-1/stream',
    )
  })
})
