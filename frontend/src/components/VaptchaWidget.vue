<template>
  <span v-if="false" />
</template>

<script setup lang="ts">
import { onBeforeUnmount } from 'vue'

interface VaptchaInstance {
  listen(event: string, callback: () => void): void
  render(): void
  validate(): void
  reset(): void
  getServerToken(): { token: string; server?: string }
}

declare global {
  interface Window {
    vaptcha?: (config: Record<string, unknown>) => Promise<VaptchaInstance>
  }
}

const props = defineProps<{ vid: string; scene?: number }>()
const emit = defineEmits<{ verify: [token: string]; error: [] }>()

let instance: VaptchaInstance | null = null
let pending: Promise<string | null> | null = null
let cancelPending: (() => void) | null = null
let scriptPromise: Promise<void> | null = null

function loadScript(): Promise<void> {
  if (window.vaptcha) return Promise.resolve()
  if (scriptPromise) return scriptPromise

  const pendingScript = new Promise<void>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://v.vaptcha.com/v3.js'
    script.async = true
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('Failed to load VAPTCHA SDK'))
    document.head.appendChild(script)
  })
  scriptPromise = pendingScript
  void pendingScript.catch(() => {
    if (scriptPromise === pendingScript) scriptPromise = null
  })
  return pendingScript
}

function serializeServerToken(captcha: VaptchaInstance): string | null {
  const serverToken = captcha.getServerToken()
  if (!serverToken?.token) return null
  return JSON.stringify({
    token: serverToken.token,
    server: serverToken.server || ''
  })
}

function createVerificationPromise(): Promise<string | null> {
  return new Promise((resolve, reject) => {
    let settled = false

    const finish = (callback: () => void): void => {
      if (settled) return
      settled = true
      if (cancelPending === cancel) cancelPending = null
      instance?.reset()
      instance = null
      callback()
    }
    const cancel = (): void => finish(() => resolve(null))

    cancelPending = cancel
    void loadScript()
      .then(async () => {
        if (cancelPending !== cancel) return
        if (!window.vaptcha) throw new Error('VAPTCHA SDK is unavailable')

        const captcha = await window.vaptcha({
          vid: props.vid,
          mode: 'invisible',
          scene: props.scene ?? 0,
          lang: 'auto',
          area: 'auto'
        })
        if (cancelPending !== cancel) {
          captcha.reset()
          return
        }

        instance = captcha
        captcha.listen('pass', () => {
          const token = serializeServerToken(captcha)
          if (!token) {
            finish(() => reject(new Error('VAPTCHA verification returned no token')))
            return
          }
          emit('verify', token)
          finish(() => resolve(token))
        })
        captcha.listen('close', cancel)
        captcha.render()
        captcha.validate()
      })
      .catch((error: unknown) => {
        finish(() => reject(error))
      })
  })
}

function verify(): Promise<string | null> {
  if (pending) return pending
  pending = createVerificationPromise().finally(() => {
    pending = null
  })
  return pending
}

function reset(): void {
  instance?.reset()
  instance = null
  cancelPending?.()
  cancelPending = null
}

onBeforeUnmount(reset)
defineExpose({ verify, reset })
</script>
