<template>
  <div class="auth-page" :class="{ 'auth-page-paper': props.variant === 'paper' }">
    <div class="auth-backdrop" aria-hidden="true"></div>

    <main class="auth-shell">
      <router-link to="/" class="auth-brand" :aria-label="siteName">
        <span class="auth-logo-frame">
          <img :src="siteLogo || '/logo.svg'" alt="Logo" />
        </span>
        <span class="auth-site-name">{{ siteName }}</span>
        <span class="auth-site-subtitle">{{ siteSubtitle }}</span>
      </router-link>

      <section class="auth-card">
        <span class="auth-card-tape" aria-hidden="true"></span>
        <slot />
      </section>

      <div class="auth-footer">
        <slot name="footer" />
      </div>

      <p class="auth-copyright">&copy; {{ currentYear }} {{ siteName }}. All rights reserved.</p>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const props = withDefaults(defineProps<{
  variant?: 'default' | 'paper'
}>(), {
  variant: 'default'
})

const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>

<style scoped>
.auth-page.auth-page-paper {
  position: relative;
  display: flex;
  min-height: 100vh;
  min-height: 100svh;
  overflow-x: hidden;
  padding: 32px 18px;
  color: #111827;
  background: #f8fbfb;
  isolation: isolate;
}

.auth-page-paper .auth-backdrop {
  position: fixed;
  inset: 0;
  z-index: -1;
  pointer-events: none;
  background:
    radial-gradient(circle at 98% 6%, rgba(45, 212, 191, 0.16), transparent 28rem),
    radial-gradient(circle at 4% 92%, rgba(45, 212, 191, 0.1), transparent 30rem),
    linear-gradient(rgba(148, 163, 184, 0.075) 1px, transparent 1px),
    linear-gradient(90deg, rgba(148, 163, 184, 0.075) 1px, transparent 1px);
  background-size: auto, auto, 48px 48px, 48px 48px;
}

.auth-page-paper .auth-shell {
  width: min(100%, 460px);
  margin: auto;
  padding: 28px 0;
}

.auth-brand {
  display: flex;
  align-items: center;
  flex-direction: column;
  margin-bottom: 28px;
  text-align: center;
}

.auth-page-paper .auth-logo-frame {
  display: inline-flex;
  width: 72px;
  height: 72px;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  margin-bottom: 17px;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 20px;
  background: #07101f;
  box-shadow: 0 15px 30px rgba(13, 148, 136, 0.16);
  transition: transform 180ms ease, box-shadow 180ms ease;
}

.auth-page-paper .auth-brand:hover .auth-logo-frame {
  transform: translateY(-2px);
  box-shadow: 0 18px 34px rgba(13, 148, 136, 0.21);
}

.auth-logo-frame img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.auth-page-paper .auth-site-name {
  color: #0f9f91;
  font-size: 32px;
  font-weight: 750;
  letter-spacing: -0.035em;
  line-height: 1.15;
  overflow-wrap: anywhere;
}

.auth-page-paper .auth-site-subtitle {
  max-width: 420px;
  margin-top: 10px;
  color: #677386;
  font-size: 13px;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.auth-page-paper .auth-card {
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 22px;
  padding: 34px;
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 22px 55px rgba(15, 23, 42, 0.09);
}

.auth-footer {
  min-height: 20px;
  margin-top: 24px;
  color: #64748b;
  font-size: 14px;
  text-align: center;
}

.auth-copyright {
  margin: 42px 0 0;
  color: #9aa5b5;
  font-size: 11px;
  text-align: center;
}

:global(.dark) .auth-page {
  color: #f8fafc;
  background: #07101e;
}

:global(.dark) .auth-backdrop {
  background:
    radial-gradient(circle at 98% 6%, rgba(20, 184, 166, 0.14), transparent 28rem),
    radial-gradient(circle at 4% 92%, rgba(20, 184, 166, 0.08), transparent 30rem),
    linear-gradient(rgba(148, 163, 184, 0.055) 1px, transparent 1px),
    linear-gradient(90deg, rgba(148, 163, 184, 0.055) 1px, transparent 1px);
  background-size: auto, auto, 48px 48px, 48px 48px;
}

:global(.dark) .auth-card {
  border-color: rgba(71, 85, 105, 0.5);
  background: rgba(15, 23, 42, 0.94);
  box-shadow: 0 24px 56px rgba(0, 0, 0, 0.3);
}

:global(.dark) .auth-site-name {
  color: #5eead4;
}

:global(.dark) .auth-site-subtitle,
:global(.dark) .auth-footer {
  color: #94a3b8;
}

@media (max-width: 520px) {
  .auth-page {
    align-items: flex-start;
    padding: 22px 14px;
  }

  .auth-shell {
    padding: 14px 0 24px;
  }

  .auth-brand {
    margin-bottom: 22px;
  }

  .auth-logo-frame {
    width: 62px;
    height: 62px;
    margin-bottom: 14px;
    border-radius: 18px;
  }

  .auth-site-name {
    font-size: 28px;
  }

  .auth-card {
    border-radius: 18px;
    padding: 26px 20px;
  }

  .auth-copyright {
    margin-top: 32px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .auth-logo-frame {
    transition: none;
  }
}

/* Paper dashboard theme */
.auth-page {
  color: #302b24;
  background: #f3ecdc;
  font-family: ui-rounded, "Arial Rounded MT Bold", "PingFang SC", "Microsoft YaHei", sans-serif;
}

.auth-backdrop {
  background:
    linear-gradient(rgba(205, 188, 139, 0.4) 1px, transparent 1px),
    linear-gradient(90deg, rgba(205, 188, 139, 0.4) 1px, transparent 1px);
  background-size: 36px 36px;
}

.auth-shell {
  width: min(100%, 480px);
}

.auth-logo-frame {
  width: 74px;
  height: 74px;
  border: 3px solid #302b24;
  border-radius: 15px;
  background: #fbf7ed;
  box-shadow: 5px 5px 0 #302b24;
  transform: rotate(-1.5deg);
}

.auth-brand:hover .auth-logo-frame {
  transform: rotate(1deg) translateY(-2px);
  box-shadow: 6px 7px 0 #302b24;
}

.auth-site-name {
  color: #302b24;
  font-family: Georgia, "Times New Roman", "Songti SC", "Noto Serif SC", serif;
  font-size: 36px;
  font-style: italic;
  font-weight: 800;
  letter-spacing: -0.045em;
  text-decoration: underline;
  text-decoration-thickness: 2px;
  text-underline-offset: 6px;
}

.auth-site-subtitle {
  margin-top: 16px;
  color: #5e564a;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.03em;
}

.auth-card {
  position: relative;
  border: 3px solid #302b24;
  border-radius: 16px;
  padding: 36px;
  background: #fbf7ed;
  box-shadow: 7px 7px 0 #302b24;
  transform: rotate(0.15deg);
}

.auth-page-paper .auth-card-tape {
  position: absolute;
  top: -12px;
  right: 50px;
  width: 78px;
  height: 24px;
  background: rgba(133, 119, 86, 0.24);
  transform: rotate(2deg);
}

.auth-page-paper .auth-footer {
  color: #514a40;
  font-weight: 650;
}

.auth-page-paper .auth-copyright {
  color: #766d5e;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

:global(.dark) .auth-page.auth-page-paper {
  color: #eee5d3;
  background: #24211c;
}

:global(.dark) .auth-page-paper .auth-backdrop {
  background:
    linear-gradient(rgba(143, 129, 91, 0.28) 1px, transparent 1px),
    linear-gradient(90deg, rgba(143, 129, 91, 0.28) 1px, transparent 1px);
  background-size: 36px 36px;
}

:global(.dark) .auth-page-paper .auth-logo-frame,
:global(.dark) .auth-page-paper .auth-card {
  border-color: #eee5d3;
  color: #eee5d3;
  background: #302c25;
  box-shadow: 6px 6px 0 rgba(238, 229, 211, 0.45);
}

:global(.dark) .auth-page-paper .auth-site-name,
:global(.dark) .auth-page-paper .auth-site-subtitle,
:global(.dark) .auth-page-paper .auth-footer,
:global(.dark) .auth-page-paper .auth-copyright {
  color: #eee5d3;
}

@media (max-width: 520px) {
  .auth-page.auth-page-paper {
    padding: 18px 12px;
  }

  .auth-page-paper .auth-shell {
    padding-top: 18px;
  }

  .auth-page-paper .auth-logo-frame {
    width: 62px;
    height: 62px;
    border-width: 2px;
    border-radius: 12px;
    box-shadow: 4px 4px 0 #302b24;
  }

  .auth-page-paper .auth-site-name {
    font-size: 30px;
  }

  .auth-page-paper .auth-card {
    border-width: 2px;
    border-radius: 13px;
    padding: 28px 20px;
    box-shadow: 5px 5px 0 #302b24;
    transform: none;
  }
}
</style>
