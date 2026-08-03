// 从 res/drawable/ic_vohive_agent.xml 的矢量图形直接光栅化出 PWA PNG 图标，
// 不依赖任何外部包（Node 内置 zlib）。
// 用法：node scripts/gen-icons.mjs
import { deflateSync } from 'node:zlib'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const OUT = join(dirname(fileURLToPath(import.meta.url)), '../public/icons')

// 源图形：48x48 视口，圆角矩形底 #4F46E5，白色 "V" 多边形
const BG = [0x4f, 0x46, 0xe5]
const FG = [0xff, 0xff, 0xff]
const V_POLY = [[13, 13], [18, 13], [24, 30], [30, 13], [35, 13], [26.5, 35], [21.5, 35]]

function pointInPoly(x, y, poly) {
  let inside = false
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, yi] = poly[i]
    const [xj, yj] = poly[j]
    if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) inside = !inside
  }
  return inside
}

function render(size, { maskable = false } = {}) {
  const px = new Uint8Array(size * size * 3)
  // maskable 图标需要安全区：背景铺满，logo 缩到 80% 居中
  const scale = maskable ? 0.8 : 1
  const offset = (size * (1 - scale)) / 2
  const corner = 4 / 48 // 源圆角半径
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      let color
      if (maskable) {
        color = BG
      } else {
        // 圆角矩形判定（归一化坐标）
        const u = (x + 0.5) / size
        const v = (y + 0.5) / size
        const r = corner
        const inRect =
          (u >= r && u <= 1 - r) || (v >= r && v <= 1 - r) ||
          (u - r) ** 2 + (v - r) ** 2 <= r ** 2 ||
          (u - (1 - r)) ** 2 + (v - r) ** 2 <= r ** 2 ||
          (u - r) ** 2 + (v - (1 - r)) ** 2 <= r ** 2 ||
          (u - (1 - r)) ** 2 + (v - (1 - r)) ** 2 <= r ** 2
        color = inRect ? BG : BG // 非 maskable 也铺满，背景色即主题色
      }
      // 映射回 48 视口判定 V
      const sx = ((x + 0.5 - offset) / (size * scale)) * 48
      const sy = ((y + 0.5 - offset) / (size * scale)) * 48
      if (sx >= 0 && sx < 48 && sy >= 0 && sy < 48 && pointInPoly(sx, sy, V_POLY)) color = FG
      const i = (y * size + x) * 3
      px[i] = color[0]; px[i + 1] = color[1]; px[i + 2] = color[2]
    }
  }
  return px
}

// ---- 最小 PNG 编码 ----
function crc32(buf) {
  let table = crc32.table
  if (!table) {
    table = crc32.table = new Int32Array(256)
    for (let n = 0; n < 256; n++) {
      let c = n
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
      table[n] = c
    }
  }
  let c = ~0
  for (const b of buf) c = crc32.table[(c ^ b) & 0xff] ^ (c >>> 8)
  return ~c >>> 0
}

function chunk(type, data) {
  const len = Buffer.alloc(4); len.writeUInt32BE(data.length)
  const body = Buffer.concat([Buffer.from(type), data])
  const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(body))
  return Buffer.concat([len, body, crc])
}

function encodePng(size, rgb) {
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(size, 0); ihdr.writeUInt32BE(size, 4)
  ihdr[8] = 8; ihdr[9] = 2 // 8-bit RGB
  const raw = Buffer.alloc(size * (size * 3 + 1))
  for (let y = 0; y < size; y++) {
    raw[y * (size * 3 + 1)] = 0 // filter: none
    Buffer.from(rgb.buffer, y * size * 3, size * 3).copy(raw, y * (size * 3 + 1) + 1)
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0))
  ])
}

mkdirSync(OUT, { recursive: true })
for (const [name, size, opts] of [
  ['icon-192.png', 192, {}],
  ['icon-512.png', 512, {}],
  ['maskable-512.png', 512, { maskable: true }]
]) {
  writeFileSync(join(OUT, name), encodePng(size, render(size, opts)))
  console.log('wrote', name)
}
