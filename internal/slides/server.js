// memo のスライド描画サーバー。Chrome を起動したまま、頼まれるたびにスライドを PNG にする。
//
// 使い方: node server.js <marp-cli のフォルダ>
//   標準入力に1行1件の JSON {"markdown", "dir", "out"} を受け取り、
//   標準出力に1行1件の JSON {"pages": [PNG のパス…]} か {"error": "…"} を返す。
//
// 中身の変わらないスライドは描き直さない (PNG の名前を中身のハッシュにする)。
'use strict'
const crypto = require('crypto')
const fs = require('fs')
const path = require('path')
const readline = require('readline')
const { pathToFileURL } = require('url')

const cliDir = process.argv[2]
const fromCli = (name) => require(path.join(cliDir, 'node_modules', name))
const { Marp } = fromCli('@marp-team/marp-core')
const puppeteer = fromCli('puppeteer-core')

const WIDTH = 1280
const HEIGHT = 720

function findChrome() {
  const candidates = [
    process.env.CHROME_PATH,
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
    '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
    '/usr/bin/google-chrome',
    '/usr/bin/google-chrome-stable',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/usr/bin/microsoft-edge',
  ]
  const found = candidates.find((p) => p && fs.existsSync(p))
  if (!found) throw new Error('Chrome が見つかりません (CHROME_PATH で場所を指定できます)')
  return found
}

let pagePromise
function page() {
  pagePromise ??= (async () => {
    const browser = await puppeteer.launch({
      executablePath: findChrome(),
      headless: true,
      args: ['--allow-file-access-from-files', '--hide-scrollbars'],
    })
    const p = await browser.newPage()
    await p.setViewport({ width: WIDTH, height: HEIGHT })
    return p
  })()
  return pagePromise
}

let previous = new Set() // 前回のスライドの PNG (端末が読み終えるまで消さない)

async function render({ markdown, dir, out }) {
  const marp = new Marp({ html: true, script: false })
  const { html, css } = marp.render(markdown)
  const slides = html.split(/(?=<svg data-marpit-svg)/).filter((s) => s.startsWith('<svg'))
  const style = `<style>${css}</style><style>
    html, body { margin: 0; padding: 0; background: transparent; }
    svg[data-marpit-svg] { display: block; width: ${WIDTH}px; height: ${HEIGHT}px; }
  </style>`
  fs.mkdirSync(out, { recursive: true })
  const base = `<base href="${pathToFileURL(dir + path.sep).href}">`
  const pages = slides.map((slide) => {
    const hash = crypto.createHash('sha1').update(css).update(slide).update(dir).digest('hex').slice(0, 16)
    return { slide, file: path.join(out, `${hash}.png`) }
  })
  const missing = pages.filter((p) => !fs.existsSync(p.file))
  if (missing.length > 0) {
    const p = await page()
    // 描き直すスライドだけを並べて読み込み、1枚ずつ撮る
    const document = `<!doctype html><html><head><meta charset="utf-8">${base}${style}</head>` +
      `<body><div class="marpit">${missing.map((m) => m.slide).join('')}</div></body></html>`
    const htmlFile = path.join(out, 'render.html')
    fs.writeFileSync(htmlFile, document)
    await p.goto(pathToFileURL(htmlFile).href, { waitUntil: 'load' })
    await p.evaluate(() => document.fonts.ready)
    const elements = await p.$$('svg[data-marpit-svg]')
    for (let i = 0; i < missing.length; i++) {
      await elements[i].screenshot({ path: missing[i].file })
    }
  }
  const current = new Set(pages.map((p) => p.file))
  for (const file of previous) {
    if (!current.has(file)) fs.rmSync(file, { force: true })
  }
  previous = current
  return pages.map((p) => p.file)
}

const input = readline.createInterface({ input: process.stdin })
let queue = Promise.resolve()
input.on('line', (line) => {
  queue = queue.then(async () => {
    let reply
    try {
      reply = { pages: await render(JSON.parse(line)) }
    } catch (error) {
      reply = { error: String(error && error.message ? error.message : error) }
    }
    process.stdout.write(JSON.stringify(reply) + '\n')
  })
})
input.on('close', async () => {
  if (pagePromise) {
    try {
      await (await pagePromise).browser().close()
    } catch {}
  }
  process.exit(0)
})
