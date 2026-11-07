// memo の描画サーバーが Chrome のページで呼ぶ。Markdown をスライドごとの HTML と CSS にする。
import { Marp } from '@marp-team/marp-core'

window.renderMarp = (markdown) => {
  const marp = new Marp({ html: true, script: false })
  const { html, css } = marp.render(markdown, { htmlAsArray: true })
  return { slides: html, css }
}
