import { reactive } from 'vue'

// 全局轻提示：任意视图 toast('已提交') / toast('失败', true)
export const toastState = reactive({ message: '', error: false, visible: false })

let timer: ReturnType<typeof setTimeout> | undefined

export function toast(message: string, error = false) {
  toastState.message = message || '操作完成'
  toastState.error = error
  toastState.visible = true
  clearTimeout(timer)
  timer = setTimeout(() => { toastState.visible = false }, 3000)
}
