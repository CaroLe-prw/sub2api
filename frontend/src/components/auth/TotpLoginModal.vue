<template>
  <div class="totp-modal" role="dialog" aria-modal="true" aria-labelledby="totp-login-title">
    <div class="totp-modal-positioner">
      <div class="totp-modal-backdrop"></div>

      <div class="totp-dialog">
        <span class="totp-dialog-tape" aria-hidden="true"></span>
        <!-- Header -->
        <div class="totp-header">
          <div class="totp-shield">
            <svg class="totp-shield-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.8">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
            </svg>
          </div>
          <h3 id="totp-login-title" class="totp-title">
            {{ t('profile.totp.loginTitle') }}
          </h3>
          <p class="totp-hint">
            {{ t('profile.totp.loginHint') }}
          </p>
          <p v-if="userEmailMasked" class="totp-account">
            {{ userEmailMasked }}
          </p>
        </div>

        <!-- Code Input -->
        <div class="totp-code-section">
          <!-- Hidden input for password manager autofill (autocomplete="one-time-code") -->
          <input
            ref="hiddenOtpInputRef"
            type="text"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            class="pointer-events-none absolute left-0 top-0 h-px w-px opacity-0"
            aria-hidden="true"
            tabindex="-1"
            @input="handleHiddenOtpInput"
          />
          <div class="totp-code-row">
            <input
              v-for="(_, index) in 6"
              :key="index"
              :ref="(el) => setInputRef(el, index)"
              type="text"
              maxlength="1"
              inputmode="numeric"
              pattern="[0-9]"
              autocomplete="off"
              class="totp-code-input"
              :disabled="verifying"
              @input="handleCodeInput($event, index)"
              @keydown="handleKeydown($event, index)"
              @paste="handlePaste"
            />
          </div>
          <!-- Loading indicator -->
          <div v-if="verifying" class="totp-verifying">
            <div class="totp-spinner"></div>
            {{ t('common.verifying') }}
          </div>
        </div>

        <!-- Cancel button only -->
        <button
          type="button"
          class="totp-cancel-button"
          :disabled="verifying"
          @click="$emit('cancel')"
        >
          {{ t('common.cancel') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'

defineProps<{
  tempToken: string
  userEmailMasked?: string
}>()

const emit = defineEmits<{
  verify: [code: string]
  cancel: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const verifying = ref(false)
const code = ref<string[]>(['', '', '', '', '', ''])
const inputRefs = ref<(HTMLInputElement | null)[]>([])
const hiddenOtpInputRef = ref<HTMLInputElement | null>(null)

// Watch for code changes and auto-submit when 6 digits are entered
watch(
  () => code.value.join(''),
  (newCode) => {
    if (newCode.length === 6 && !verifying.value) {
      emit('verify', newCode)
    }
  }
)

defineExpose({
  setVerifying: (value: boolean) => { verifying.value = value },
  setError: (message: string) => {
    if (message) {
      appStore.showError(message)
    }
    code.value = ['', '', '', '', '', '']
    // Clear input DOM values
    inputRefs.value.forEach(input => {
      if (input) input.value = ''
    })
    // Clear hidden autofill input
    if (hiddenOtpInputRef.value) {
      hiddenOtpInputRef.value.value = ''
    }
    nextTick(() => {
      inputRefs.value[0]?.focus()
    })
  }
})

const setInputRef = (el: any, index: number) => {
  inputRefs.value[index] = el as HTMLInputElement | null
}

const handleCodeInput = (event: Event, index: number) => {
  const input = event.target as HTMLInputElement
  const value = input.value.replace(/[^0-9]/g, '')
  code.value[index] = value

  if (value && index < 5) {
    nextTick(() => {
      inputRefs.value[index + 1]?.focus()
    })
  }
}

// Handle autofill from password managers via the hidden autocomplete="one-time-code" input
const handleHiddenOtpInput = (event: Event) => {
  const input = event.target as HTMLInputElement
  const digits = input.value.replace(/[^0-9]/g, '').slice(0, 6).split('')

  digits.forEach((digit, i) => {
    code.value[i] = digit
    if (inputRefs.value[i]) {
      inputRefs.value[i]!.value = digit
    }
  })

  for (let i = digits.length; i < 6; i++) {
    code.value[i] = ''
    if (inputRefs.value[i]) {
      inputRefs.value[i]!.value = ''
    }
  }
}

const handleKeydown = (event: KeyboardEvent, index: number) => {
  if (event.key === 'Backspace') {
    const input = event.target as HTMLInputElement
    // If current cell is empty and not the first, move to previous cell
    if (!input.value && index > 0) {
      event.preventDefault()
      inputRefs.value[index - 1]?.focus()
    }
    // Otherwise, let the browser handle the backspace naturally
    // The input event will sync code.value via handleCodeInput
  }
}

const handlePaste = (event: ClipboardEvent) => {
  event.preventDefault()
  const pastedData = event.clipboardData?.getData('text') || ''
  const digits = pastedData.replace(/[^0-9]/g, '').slice(0, 6).split('')

  // Update both the ref and the input elements
  digits.forEach((digit, index) => {
    code.value[index] = digit
    if (inputRefs.value[index]) {
      inputRefs.value[index]!.value = digit
    }
  })

  // Clear remaining inputs if pasted less than 6 digits
  for (let i = digits.length; i < 6; i++) {
    code.value[i] = ''
    if (inputRefs.value[i]) {
      inputRefs.value[i]!.value = ''
    }
  }

  const focusIndex = Math.min(digits.length, 5)
  nextTick(() => {
    inputRefs.value[focusIndex]?.focus()
  })
}

onMounted(() => {
  nextTick(() => {
    inputRefs.value[0]?.focus()
  })
})
</script>

<style scoped>
.totp-modal {
  position: fixed;
  inset: 0;
  z-index: 50;
  overflow-y: auto;
  color: #302b24;
}

.totp-modal-positioner {
  display: flex;
  min-height: 100%;
  align-items: center;
  justify-content: center;
  padding: 22px;
}

.totp-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(48, 43, 36, 0.62);
  backdrop-filter: blur(2px);
}

.totp-dialog {
  position: relative;
  width: min(100%, 430px);
  border: 3px solid #302b24;
  border-radius: 17px;
  padding: 34px 30px 28px;
  background-color: #fbf7ed;
  background-image:
    linear-gradient(rgba(211, 195, 151, 0.27) 1px, transparent 1px),
    linear-gradient(90deg, rgba(211, 195, 151, 0.27) 1px, transparent 1px);
  background-size: 24px 24px;
  box-shadow: 8px 8px 0 #302b24;
  transform: rotate(-0.2deg);
}

.totp-dialog-tape {
  position: absolute;
  top: -16px;
  left: 50%;
  width: 94px;
  height: 27px;
  background: rgba(255, 224, 112, 0.83);
  box-shadow: 0 3px 0 rgba(48, 43, 36, 0.14);
  transform: translateX(-50%) rotate(2deg);
}

.totp-header {
  margin-bottom: 27px;
  text-align: center;
}

.totp-shield {
  display: flex;
  width: 56px;
  height: 56px;
  align-items: center;
  justify-content: center;
  margin: 0 auto;
  border: 2px solid #302b24;
  border-radius: 13px;
  background: #ffd66f;
  box-shadow: 4px 4px 0 #302b24;
  transform: rotate(-2deg);
}

.totp-shield-icon {
  width: 28px;
  height: 28px;
  color: #302b24;
}

.totp-title {
  margin-top: 21px;
  color: #302b24;
  font-family: Georgia, "Times New Roman", "Songti SC", "Noto Serif SC", serif;
  font-size: 27px;
  font-weight: 800;
  line-height: 1.2;
}

.totp-hint {
  margin-top: 11px;
  color: #6c6255;
  font-size: 14px;
  line-height: 1.65;
}

.totp-account {
  margin-top: 3px;
  color: #302b24;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 14px;
  font-weight: 800;
}

.totp-code-section {
  margin-bottom: 27px;
}

.totp-code-row {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 8px;
}

.totp-code-input {
  width: 100%;
  min-width: 0;
  height: 56px;
  border: 2px solid #302b24;
  border-radius: 8px;
  outline: none;
  color: #302b24;
  background: #fffaf0;
  box-shadow: 2px 2px 0 #302b24;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 21px;
  font-weight: 800;
  text-align: center;
  transition: transform 120ms ease, background-color 120ms ease, box-shadow 120ms ease;
}

.totp-code-input:focus {
  border-color: #302b24;
  background: #fff0a8;
  box-shadow: 3px 3px 0 #e34a2c;
  transform: translate(-1px, -1px);
}

.totp-code-input:disabled {
  cursor: wait;
  opacity: 0.58;
}

.totp-verifying {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  margin-top: 16px;
  color: #6c6255;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  font-weight: 700;
}

.totp-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid rgba(48, 43, 36, 0.25);
  border-top-color: #e34a2c;
  border-radius: 50%;
  animation: totp-spin 750ms linear infinite;
}

.totp-cancel-button {
  width: 100%;
  min-height: 49px;
  border: 2px solid #302b24;
  border-radius: 10px;
  color: #302b24;
  background: #fbf7ed;
  box-shadow: 3px 3px 0 #302b24;
  font-size: 15px;
  font-weight: 800;
  transition: transform 120ms ease, box-shadow 120ms ease, background-color 120ms ease;
}

.totp-cancel-button:hover:not(:disabled) {
  background: #ffd66f;
  box-shadow: 4px 4px 0 #302b24;
  transform: translate(-1px, -1px);
}

.totp-cancel-button:active:not(:disabled) {
  box-shadow: 1px 1px 0 #302b24;
  transform: translate(2px, 2px);
}

.totp-cancel-button:disabled {
  cursor: wait;
  opacity: 0.58;
}

@keyframes totp-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 480px) {
  .totp-modal-positioner {
    padding: 18px;
  }

  .totp-dialog {
    padding: 31px 20px 24px;
    border-radius: 15px;
    box-shadow: 6px 6px 0 #302b24;
  }

  .totp-header {
    margin-bottom: 23px;
  }

  .totp-title {
    font-size: 25px;
  }

  .totp-code-row {
    gap: 6px;
  }

  .totp-code-input {
    height: 52px;
    font-size: 19px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .totp-code-input,
  .totp-cancel-button {
    transition: none;
  }
}
</style>
