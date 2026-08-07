<template>
  <button
    type="button"
    class="btn-secondary w-full"
    :disabled="loading"
    @click="verify"
  >
    {{ loading ? $t('auth.captchaVerifying') : $t('auth.captchaClickToVerify') }}
  </button>
</template>

<script setup lang="ts">
import { ref } from 'vue'

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
const loading = ref(false)
let instance: VaptchaInstance | null = null
let scriptPromise: Promise<void> | null = null

function loadScript(): Promise<void> {
  if (window.vaptcha) return Promise.resolve()
  if (scriptPromise) return scriptPromise
  scriptPromise = new Promise((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://v.vaptcha.com/v3.js'
    script.async = true
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('Failed to load VAPTCHA SDK'))
    document.head.appendChild(script)
  })
  return scriptPromise
}

async function getInstance(): Promise<VaptchaInstance> {
  if (instance) return instance
  await loadScript()
  if (!window.vaptcha) throw new Error('VAPTCHA SDK is unavailable')
  instance = await window.vaptcha({
    vid: props.vid,
    mode: 'click',
    scene: props.scene ?? 0,
    style: 'light',
    lang: 'auto',
    area: 'auto'
  })
  instance.listen('pass', () => {
    const serverToken = instance?.getServerToken()
    const token = serverToken?.token
      ? JSON.stringify({ token: serverToken.token, server: serverToken.server || '' })
      : ''
    loading.value = false
    if (token) emit('verify', token)
    else emit('error')
  })
  instance.listen('close', () => {
    loading.value = false
    instance?.reset()
  })
  instance.render()
  return instance
}

async function verify(): Promise<string | null> {
  loading.value = true
  try {
    const captcha = await getInstance()
    return await new Promise((resolve) => {
      captcha.listen('pass', () => {
        const serverToken = captcha.getServerToken()
        resolve(serverToken.token ? JSON.stringify({ token: serverToken.token, server: serverToken.server || '' }) : null)
      })
      captcha.listen('close', () => resolve(null))
      captcha.validate()
    })
  } catch {
    loading.value = false
    emit('error')
    return null
  }
}

function reset(): void {
  loading.value = false
  instance?.reset()
}

defineExpose({ verify, reset })
</script>
