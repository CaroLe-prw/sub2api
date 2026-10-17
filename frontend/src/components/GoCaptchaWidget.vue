<template>
  <BaseDialog :show="visible" :title="t('auth.goCaptcha.title')" width="narrow" @close="reset">
    <div ref="container" class="flex w-full flex-col items-center gap-3" :aria-busy="loading || checking">
      <p v-if="loading" role="status" class="py-12 text-sm text-gray-500">{{ t('auth.goCaptcha.loading') }}</p>
      <div v-if="challenge && !loading" :class="{ 'pointer-events-none opacity-60': checking }">
        <Slide
          :key="challenge.id"
          :config="{ width: challenge.width * scale, height: challenge.height * scale, horizontalPadding: 0, showTheme: false, title: t('auth.goCaptcha.slide') }"
          :data="{
            image: challenge.image, thumb: challenge.thumb,
            thumbX: challenge.thumb_x * scale, thumbY: challenge.thumb_y * scale,
            thumbWidth: challenge.thumb_width * scale, thumbHeight: challenge.thumb_height * scale
          }"
          :events="{ confirm: confirmAnswer, refresh: refresh, close: reset }"
        />
      </div>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <button type="button" class="text-sm text-primary-600 disabled:opacity-50" :disabled="loading || checking" @click="refresh">
        {{ t('auth.goCaptcha.retry') }}
      </button>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef } from 'vue'
import { useElementSize } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { Slide } from 'go-captcha-vue'
import 'go-captcha-vue/dist/style.css'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { createGoCaptchaChallenge, checkGoCaptchaChallenge, type GoCaptchaChallenge } from '@/api/goCaptcha'

const { t } = useI18n()
const visible = ref(false)
const loading = ref(false)
const checking = ref(false)
const error = ref('')
const challenge = shallowRef<GoCaptchaChallenge | null>(null)
const container = ref<HTMLElement | null>(null)
const { width } = useElementSize(container)
const scale = computed(() => challenge.value && width.value > 0 ? Math.min(1, width.value / challenge.value.width) : 1)
let pending: Promise<string | null> | null = null
let resolvePending: ((token: string | null) => void) | null = null
let request: AbortController | null = null
let generation = 0

async function refresh(): Promise<void> {
  if (!visible.value) return
  const current = ++generation
  request?.abort()
  request = new AbortController()
  loading.value = true
  checking.value = false
  challenge.value = null
  try {
    const data = await createGoCaptchaChallenge(request.signal)
    if (current === generation) challenge.value = data
  } catch {
    if (current === generation) error.value = t('auth.goCaptcha.unavailable')
  } finally {
    if (current === generation) loading.value = false
  }
}

async function confirmAnswer(point: { x: number; y: number }): Promise<void> {
  if (!challenge.value || checking.value || loading.value) return
  const current = generation
  checking.value = true
  error.value = ''
  request?.abort()
  request = new AbortController()
  try {
    const token = await checkGoCaptchaChallenge(challenge.value.id, {
      x: point.x / scale.value, y: point.y / scale.value
    }, request.signal)
    if (current === generation) finish(token)
  } catch {
    if (current !== generation) return
    error.value = t('auth.goCaptcha.failed')
    await refresh()
  } finally {
    if (current === generation) checking.value = false
  }
}

function finish(token: string | null): void {
  ++generation
  request?.abort()
  request = null
  visible.value = false
  loading.value = false
  checking.value = false
  challenge.value = null
  const resolve = resolvePending
  resolvePending = null
  pending = null
  resolve?.(token)
}

function verify(): Promise<string | null> {
  if (pending) return pending
  pending = new Promise((resolve) => { resolvePending = resolve })
  visible.value = true
  error.value = ''
  void refresh()
  return pending
}

function reset(): void { finish(null) }
onBeforeUnmount(reset)
defineExpose({ verify, reset })
</script>
