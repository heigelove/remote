const CODE_VK: Record<string, number> = {
  Enter: 0x0d,
  NumpadEnter: 0x0d,
  Backspace: 0x08,
  Delete: 0x2e,
  Tab: 0x09,
  Escape: 0x1b,
  Space: 0x20,
  ArrowLeft: 0x25,
  ArrowUp: 0x26,
  ArrowRight: 0x27,
  ArrowDown: 0x28,
  Home: 0x24,
  End: 0x23,
  PageUp: 0x21,
  PageDown: 0x22,
  Insert: 0x2d,
  ControlLeft: 0xa2,
  ControlRight: 0xa3,
  ShiftLeft: 0xa0,
  ShiftRight: 0xa1,
  AltLeft: 0xa4,
  AltRight: 0xa5,
  MetaLeft: 0x5b,
  MetaRight: 0x5c,
  ContextMenu: 0x5d,
  CapsLock: 0x14,
  NumLock: 0x90,
  ScrollLock: 0x91,
  Pause: 0x13,
  PrintScreen: 0x2c,
  Minus: 0xbd,
  Equal: 0xbb,
  BracketLeft: 0xdb,
  BracketRight: 0xdd,
  Backslash: 0xdc,
  Semicolon: 0xba,
  Quote: 0xde,
  Backquote: 0xc0,
  Comma: 0xbc,
  Period: 0xbe,
  Slash: 0xbf,
  Numpad0: 0x60,
  Numpad1: 0x61,
  Numpad2: 0x62,
  Numpad3: 0x63,
  Numpad4: 0x64,
  Numpad5: 0x65,
  Numpad6: 0x66,
  Numpad7: 0x67,
  Numpad8: 0x68,
  Numpad9: 0x69,
  NumpadAdd: 0x6b,
  NumpadSubtract: 0x6d,
  NumpadMultiply: 0x6a,
  NumpadDivide: 0x6f,
  NumpadDecimal: 0x6e,
}

const EXTENDED = new Set([
  'Delete',
  'Insert',
  'Home',
  'End',
  'PageUp',
  'PageDown',
  'ArrowLeft',
  'ArrowRight',
  'ArrowUp',
  'ArrowDown',
  'NumpadEnter',
  'NumpadDivide',
  'ControlRight',
  'AltRight',
  'MetaLeft',
  'MetaRight',
  'ContextMenu',
])

function fillLettersAndDigits() {
  for (let i = 0; i < 26; i++) CODE_VK['Key' + String.fromCharCode(65 + i)] = 0x41 + i
  for (let i = 0; i < 10; i++) CODE_VK['Digit' + i] = 0x30 + i
  for (let i = 1; i <= 24; i++) CODE_VK['F' + i] = 0x6f + i // F1=0x70
}

fillLettersAndDigits()

export function vkFromKeyboardEvent(e: KeyboardEvent): number {
  if (CODE_VK[e.code] != null) return CODE_VK[e.code]
  if (e.key === 'Enter') return 0x0d
  if (e.key === 'Backspace') return 0x08
  if (e.key === 'Delete') return 0x2e
  if (e.key === 'Tab') return 0x09
  if (e.key === 'Escape') return 0x1b
  if (e.key === ' ') return 0x20
  if (e.keyCode) return e.keyCode
  if (e.key.length === 1) {
    const c = e.key.toUpperCase().charCodeAt(0)
    if (c >= 65 && c <= 90) return c
    if (c >= 48 && c <= 57) return c
  }
  return 0
}

export function isExtendedKey(e: KeyboardEvent): boolean {
  return EXTENDED.has(e.code)
}

export function keyPayload(e: KeyboardEvent, action: 'keydown' | 'keyup') {
  const vk = vkFromKeyboardEvent(e)
  const payload: Record<string, unknown> = {
    action,
    vk,
    ctrl: e.ctrlKey,
    alt: e.altKey,
    shift: e.shiftKey,
    meta: e.metaKey,
    extended: isExtendedKey(e),
    repeat: e.repeat,
  }
  const printable = e.key.length === 1 && !e.ctrlKey && !e.altKey && !e.metaKey
  if (printable && /[^\x00-\x7F]/.test(e.key)) {
    payload.char = e.key
  }
  return payload
}
