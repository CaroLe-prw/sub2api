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
let pending: Promise<string | null> | null = null
let cancelPending: (() => void) | null = null
let scriptPromise: Promise<void> | null = null

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
          lang: 'auto',
          area: 'auto'
        })
        if (cancelPending !== cancel) {
          captcha.reset()
          return
        }

        instance = captcha
        const result = await captcha.validate()
        if (cancelPending !== cancel) return
        if (!result) {
          cancel()
          return
        }
        const proof = serializeProof(result)
        if (!proof) {
          finish(() => reject(new Error('VAPTCHA verification returned an incomplete proof')))
          return
        }
        emit('verify', proof)
        finish(() => resolve(proof))
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
