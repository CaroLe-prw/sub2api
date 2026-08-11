<template>
  <span v-if="false" />
</template>

<script setup lang="ts">
import { onBeforeUnmount } from 'vue'

interface VaptchaInstance {
  validate(): Promise<VaptchaValidationResult | null>
  reset(): void
}

interface VaptchaValidationResult {
  token?: string
  knock?: string
  dfu?: string
  ip?: string
}

declare global {
  interface Window {
    vaptcha?: (config: Record<string, unknown>) => Promise<VaptchaInstance>
  }
}

const props = defineProps<{ vid: string; scene?: number }>()
const emit = defineEmits<{ verify: [token: string]; error: [] }>()

let instance: VaptchaInstance | null = null
let instancePromise: Promise<VaptchaInstance> | null = null
let pending: Promise<string | null> | null = null
let cancelPending: (() => void) | null = null
let scriptPromise: Promise<void> | null = null
let disposed = false

function loadScript(): Promise<void> {
  if (window.vaptcha) return Promise.resolve()
  if (scriptPromise) return scriptPromise

  const pendingScript = new Promise<void>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://c4.vaptcha.com/src/v4.js'
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

function ensureInstance(): Promise<VaptchaInstance> {
  if (instance) return Promise.resolve(instance)
  if (instancePromise) return instancePromise

  const initializing = loadScript()
    .then(() => {
      if (!window.vaptcha) throw new Error('VAPTCHA SDK is unavailable')
      return window.vaptcha({
        vid: props.vid,
        lang: 'auto',
        area: 'auto'
      })
    })
    .then(
      (captcha) => {
        instancePromise = null
        if (disposed) {
          captcha.reset()
          throw new Error('VAPTCHA widget has been disposed')
        }
        instance = captcha
        return captcha
      },
      (error: unknown) => {
        instancePromise = null
        throw error
      }
    )

  instancePromise = initializing
  return initializing
}

function serializeProof(result: VaptchaValidationResult): string | null {
  if (!result.token || !result.knock) return null
  return JSON.stringify({
    token: result.token,
    knock: result.knock,
    dfu: result.dfu || '',
    ip: result.ip || ''
  })
}

function createVerificationPromise(): Promise<string | null> {
  return new Promise((resolve, reject) => {
    let settled = false

    const finish = (callback: () => void): void => {
      if (settled) return
      settled = true
      if (cancelPending === cancel) cancelPending = null
      callback()
    }
    const cancel = (): void => finish(() => resolve(null))

    cancelPending = cancel
    void ensureInstance()
      .then(async (captcha) => {
        if (cancelPending !== cancel) return
        const result = await captcha.validate()
        if (cancelPending !== cancel) return
        if (!result) {
          captcha.reset()
          cancel()
          return
        }
        const proof = serializeProof(result)
        if (!proof) {
          captcha.reset()
          finish(() => reject(new Error('VAPTCHA verification returned an incomplete proof')))
          return
        }
        emit('verify', proof)
        finish(() => resolve(proof))
      })
      .catch((error: unknown) => {
        if (settled) return
        instance?.reset()
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
  cancelPending?.()
  cancelPending = null
}

function dispose(): void {
  disposed = true
  reset()
  instance = null
  instancePromise = null
}

onBeforeUnmount(dispose)
defineExpose({ verify, reset })
</script>
