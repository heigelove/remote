export const STREAM_QUALITIES = [
  { id: 'native', label: '原始' },
  { id: '1080p', label: '1080p' },
  { id: '720p', label: '720p' },
  { id: '480p', label: '480p' },
] as const

const KEY = 'remote.streamQuality'

export function normalizeStreamQuality(q: string | null | undefined) {
  switch ((q || '').toLowerCase()) {
    case 'native':
    case 'original':
    case 'source':
    case 'raw':
      return 'native'
    case '720p':
    case 'hd':
    case '720':
      return '720p'
    case '480p':
    case 'sd':
    case '480':
      return '480p'
    default:
      return '1080p'
  }
}

export function loadStreamQuality() {
  try {
    return normalizeStreamQuality(localStorage.getItem(KEY))
  } catch {
    return '1080p'
  }
}

export function saveStreamQuality(q: string) {
  const v = normalizeStreamQuality(q)
  try {
    localStorage.setItem(KEY, v)
  } catch {
    // ignore
  }
  return v
}
