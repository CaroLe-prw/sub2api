<template>
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe v-if="isHomeContentUrl" :src="homeContent.trim()" class="h-screen w-full border-0" allowfullscreen></iframe>
    <!-- SECURITY: homeContent is an admin-only setting. -->
    <div v-else v-html="homeContent"></div>
  </div>

  <div v-else-if="compactHomeEnabled" data-testid="compact-home" class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200 px-4 py-4 sm:px-6 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-9 w-9 shrink-0 rounded-lg object-contain" />
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800" :title="t('home.viewDocs')">
            <Icon name="book" size="md" />
          </a>
          <button class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200">
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>
    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img :src="siteLogo || '/logo.svg'" alt="Logo" class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain" />
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700">
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
        </router-link>
      </div>
    </main>
    <footer class="min-w-0 border-t border-gray-200 px-4 py-5 text-center text-sm text-gray-500 [overflow-wrap:anywhere] sm:px-6 dark:border-dark-800 dark:text-dark-400">&copy; {{ currentYear }} {{ siteName }}</footer>
  </div>

  <div v-else class="public-home" data-testid="default-home">
    <div class="home-backdrop" aria-hidden="true"></div>
    <header class="home-header">
      <nav class="home-nav">
        <router-link to="/" class="home-logo" :aria-label="siteName">
          <span class="home-logo-frame"><img :src="siteLogo || '/logo.svg'" alt="Logo" /></span>
        </router-link>
        <div class="home-actions">
          <LocaleSwitcher />
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="home-icon-button" :title="t('home.viewDocs')"><Icon name="book" size="md" /></a>
          <button class="home-icon-button" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
            <Icon v-if="isDark" name="sun" size="md" /><Icon v-else name="moon" size="md" />
          </button>
          <router-link v-if="isAuthenticated" :to="dashboardPath" class="home-session-button">
            <span class="home-user-initial">{{ userInitial }}</span><span>{{ t('home.dashboard') }}</span><Icon name="externalLink" size="xs" />
          </router-link>
          <router-link v-else to="/login" class="home-session-button">{{ t('home.login') }}</router-link>
        </div>
      </nav>
    </header>

    <main class="home-main">
      <div class="home-content">
        <div class="paper-section-label" aria-hidden="true">
          <span>00</span>
          <span>API FORWARDING DASHBOARD</span>
        </div>
        <section class="home-hero">
          <div class="hero-copy">
            <h1 class="hero-title">{{ siteName }}</h1>
            <p class="hero-subtitle">{{ siteSubtitle }}</p>
            <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="hero-cta">
              {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
              <Icon name="arrowRight" size="md" :stroke-width="2" />
            </router-link>
          </div>
          <div class="terminal-column">
            <div class="terminal-container">
              <div class="terminal-window">
                <div class="terminal-header">
                  <div class="terminal-buttons" aria-hidden="true"><span class="btn-close"></span><span class="btn-minimize"></span><span class="btn-maximize"></span></div>
                  <span class="terminal-title">terminal</span>
                </div>
                <div class="terminal-body">
                  <div class="code-line line-1"><span class="code-prompt">$</span><span class="code-cmd">curl</span><span class="code-flag">-X POST</span><span class="code-url">/v1/messages</span></div>
                  <div class="code-line line-2"><span class="code-comment"># Routing to upstream...</span></div>
                  <div class="code-line line-3"><span class="code-success">200 OK</span><span class="code-response">{ "content": "Hello!" }</span></div>
                  <div class="code-line line-4"><span class="code-prompt">$</span><span class="cursor"></span></div>
                </div>
              </div>
            </div>
            <aside class="paper-note">{{ t('home.providers.description') }}</aside>
          </div>
        </section>

        <div class="feature-tags">
          <div v-for="tag in featureTags" :key="tag.label" class="feature-tag"><Icon :name="tag.icon" size="sm" /><span>{{ tag.label }}</span></div>
        </div>

        <section class="features-grid">
          <article v-for="feature in featureCards" :key="feature.title" class="feature-card">
            <div class="feature-card-icon" :class="feature.iconClass"><Icon :name="feature.icon" size="lg" /></div>
            <h2>{{ feature.title }}</h2><p>{{ feature.description }}</p>
          </article>
        </section>

        <section class="providers-section">
          <div class="providers-heading"><h2>{{ t('home.providers.title') }}</h2><p>{{ t('home.providers.description') }}</p></div>
          <div class="provider-list">
            <div v-for="provider in providers" :key="provider.name" class="provider-chip" :class="{ 'provider-chip-muted': provider.muted }">
              <span class="provider-mark" :class="provider.markClass">{{ provider.mark }}</span><span class="provider-name">{{ provider.name }}</span>
              <span class="provider-status" :class="{ 'provider-status-muted': provider.muted }">{{ provider.status }}</span>
            </div>
          </div>
        </section>
      </div>
    </main>

    <footer class="home-footer">
      <div class="home-footer-inner">
        <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
        <div class="home-footer-links">
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="home-footer-link">{{ t('home.docs') }}</a>
          <a :href="githubUrl" target="_blank" rel="noopener noreferrer" class="home-footer-link">GitHub</a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const featureTags = computed(() => [
  { icon: 'swap' as const, label: t('home.tags.subscriptionToApi') },
  { icon: 'shield' as const, label: t('home.tags.stickySession') },
  { icon: 'chart' as const, label: t('home.tags.realtimeBilling') }
])
const featureCards = computed(() => [
  { icon: 'server' as const, iconClass: 'feature-card-icon-blue', title: t('home.features.unifiedGateway'), description: t('home.features.unifiedGatewayDesc') },
  { icon: 'users' as const, iconClass: 'feature-card-icon-teal', title: t('home.features.multiAccount'), description: t('home.features.multiAccountDesc') },
  { icon: 'creditCard' as const, iconClass: 'feature-card-icon-violet', title: t('home.features.balanceQuota'), description: t('home.features.balanceQuotaDesc') }
])
const providers = computed(() => [
  { mark: 'C', markClass: 'provider-mark-orange', name: t('home.providers.claude'), status: t('home.providers.supported'), muted: false },
  { mark: 'G', markClass: 'provider-mark-green', name: 'GPT', status: t('home.providers.supported'), muted: false },
  { mark: 'G', markClass: 'provider-mark-blue', name: t('home.providers.gemini'), status: t('home.providers.supported'), muted: false },
  { mark: 'A', markClass: 'provider-mark-rose', name: t('home.providers.antigravity'), status: t('home.providers.supported'), muted: false },
  { mark: '+', markClass: 'provider-mark-gray', name: t('home.providers.more'), status: t('home.providers.soon'), muted: true }
])

const isDark = ref(document.documentElement.classList.contains('dark'))
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => authStore.user?.email?.charAt(0).toUpperCase() || '')
const currentYear = computed(() => new Date().getFullYear())

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}
onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
})
</script>

<style scoped>
.public-home {
  position: relative; display: flex; min-height: 100vh; flex-direction: column; overflow: hidden;
  color: #111827; background-color: #f8fbfb; isolation: isolate;
}
.home-backdrop {
  position: absolute; inset: 0; z-index: -1; pointer-events: none;
  background:
    radial-gradient(circle at 98% 10%, rgba(45, 212, 191, .16), transparent 27rem),
    radial-gradient(circle at 8% 88%, rgba(45, 212, 191, .1), transparent 31rem),
    linear-gradient(rgba(148, 163, 184, .075) 1px, transparent 1px),
    linear-gradient(90deg, rgba(148, 163, 184, .075) 1px, transparent 1px);
  background-size: auto, auto, 48px 48px, 48px 48px;
  mask-image: linear-gradient(to bottom, black 0%, black 88%, transparent 100%);
}
.home-header { position: relative; z-index: 30; padding: 20px 24px; }
.home-nav, .home-content, .home-footer-inner { width: min(100%, 1160px); margin-inline: auto; }
.home-nav { display: flex; min-height: 48px; align-items: center; justify-content: space-between; gap: 20px; }
.home-logo, .home-logo-frame { display: inline-flex; align-items: center; justify-content: center; }
.home-logo-frame {
  width: 48px; height: 48px; overflow: hidden; border: 1px solid rgba(15, 23, 42, .08); border-radius: 15px;
  background: #07101f; box-shadow: 0 10px 24px rgba(15, 23, 42, .12); transition: transform 180ms ease, box-shadow 180ms ease;
}
.home-logo:hover .home-logo-frame { transform: translateY(-2px); box-shadow: 0 14px 28px rgba(15, 23, 42, .17); }
.home-logo-frame img { width: 100%; height: 100%; object-fit: contain; }
.home-actions { display: flex; align-items: center; gap: 8px; }
.home-icon-button {
  display: inline-flex; width: 38px; height: 38px; align-items: center; justify-content: center; border-radius: 10px;
  color: #64748b; transition: color 160ms ease, background-color 160ms ease;
}
.home-icon-button:hover { color: #0f172a; background: rgba(255, 255, 255, .78); }
.home-session-button {
  display: inline-flex; min-height: 34px; align-items: center; justify-content: center; gap: 7px; border-radius: 999px;
  padding: 7px 14px; color: white; background: #111827; font-size: 12px; font-weight: 650;
  box-shadow: 0 8px 20px rgba(15, 23, 42, .12); transition: transform 160ms ease, background-color 160ms ease;
}
.home-session-button:hover { transform: translateY(-1px); background: #263244; }
.home-user-initial {
  display: inline-flex; width: 20px; height: 20px; align-items: center; justify-content: center; border-radius: 50%;
  color: white; background: linear-gradient(135deg, #2dd4bf, #0d9488); font-size: 10px;
}
.home-main { position: relative; z-index: 10; flex: 1; padding: 54px 24px 72px; }
.home-hero {
  display: grid; min-height: 385px; grid-template-columns: minmax(0, 1.02fr) minmax(380px, .98fr);
  align-items: center; gap: clamp(42px, 7vw, 96px); padding: 32px 0 68px;
}
.hero-copy { min-width: 0; }
.hero-title {
  max-width: 650px; margin: 0; color: #101827; font-size: clamp(3rem, 6vw, 5.25rem); font-weight: 750;
  letter-spacing: -.055em; line-height: .98; overflow-wrap: anywhere;
}
.hero-subtitle {
  max-width: 620px; margin: 26px 0 34px; color: #5f6b7a; font-size: clamp(1rem, 1.7vw, 1.35rem);
  line-height: 1.65; white-space: pre-wrap; overflow-wrap: anywhere;
}
.hero-cta {
  display: inline-flex; min-height: 52px; align-items: center; justify-content: center; gap: 13px; border-radius: 13px;
  padding: 13px 24px; color: white; background: linear-gradient(135deg, #18bbaa, #0d9488);
  box-shadow: 0 14px 30px rgba(13, 148, 136, .24); font-size: 15px; font-weight: 650;
  transition: transform 180ms ease, box-shadow 180ms ease;
}
.hero-cta:hover { transform: translateY(-2px); box-shadow: 0 18px 34px rgba(13, 148, 136, .3); }
.terminal-column { display: flex; min-width: 0; justify-content: flex-end; }
.terminal-container { position: relative; width: min(100%, 440px); }
.terminal-container::after {
  position: absolute; right: 8%; bottom: -28px; left: 8%; height: 48px; border-radius: 50%;
  background: rgba(15, 23, 42, .18); filter: blur(28px); content: '';
}
.terminal-window {
  position: relative; z-index: 1; width: 100%; min-height: 270px; overflow: hidden;
  border: 1px solid rgba(148, 163, 184, .12); border-radius: 17px; background: linear-gradient(145deg, #1c293d, #111b2d);
  box-shadow: 0 24px 45px rgba(15, 23, 42, .2); transition: transform 220ms ease, box-shadow 220ms ease;
}
.terminal-window:hover { transform: translateY(-4px); box-shadow: 0 30px 55px rgba(15, 23, 42, .25); }
.terminal-header {
  display: flex; min-height: 54px; align-items: center; padding: 0 18px; border-bottom: 1px solid rgba(148, 163, 184, .12);
  background: rgba(30, 41, 59, .72);
}
.terminal-buttons { display: flex; gap: 9px; }
.terminal-buttons span { width: 11px; height: 11px; border-radius: 50%; }
.btn-close { background: #f05252; } .btn-minimize { background: #f5c518; } .btn-maximize { background: #22c55e; }
.terminal-title { flex: 1; margin-right: 48px; color: #64748b; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; text-align: center; }
.terminal-body { padding: 31px 27px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; line-height: 2; }
.code-line { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; opacity: 0; animation: line-appear 450ms ease forwards; }
.line-1 { animation-delay: 200ms; } .line-2 { animation-delay: 800ms; } .line-3 { animation-delay: 1400ms; } .line-4 { animation-delay: 2000ms; }
@keyframes line-appear { from { opacity: 0; transform: translateY(5px); } to { opacity: 1; transform: translateY(0); } }
.code-prompt { color: #34d399; font-weight: 700; } .code-cmd { color: #38bdf8; } .code-flag { color: #a78bfa; }
.code-url { color: #2dd4bf; } .code-comment { color: #64748b; font-style: italic; }
.code-success { border-radius: 5px; padding: 1px 7px; color: #4ade80; background: rgba(34, 197, 94, .14); font-weight: 650; }
.code-response { color: #fbbf24; }
.cursor { display: inline-block; width: 7px; height: 15px; background: #34d399; animation: blink 1s step-end infinite; }
@keyframes blink { 0%, 50% { opacity: 1; } 51%, 100% { opacity: 0; } }
.feature-tags { display: flex; flex-wrap: wrap; justify-content: center; gap: 14px; margin: 0 auto 58px; }
.feature-tag {
  display: inline-flex; min-height: 42px; align-items: center; gap: 9px; border: 1px solid rgba(148, 163, 184, .2);
  border-radius: 999px; padding: 9px 17px; color: #3f4b5e; background: rgba(255, 255, 255, .9);
  box-shadow: 0 7px 18px rgba(15, 23, 42, .045); font-size: 13px; font-weight: 600;
}
.feature-tag svg { color: #14b8a6; }
.features-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; }
.feature-card {
  min-height: 220px; border: 1px solid rgba(148, 163, 184, .18); border-radius: 18px; padding: 28px;
  background: rgba(255, 255, 255, .82); box-shadow: 0 12px 34px rgba(15, 23, 42, .045);
  transition: transform 200ms ease, border-color 200ms ease, box-shadow 200ms ease;
}
.feature-card:hover { transform: translateY(-4px); border-color: rgba(20, 184, 166, .25); box-shadow: 0 20px 42px rgba(15, 23, 42, .075); }
.feature-card-icon {
  display: flex; width: 48px; height: 48px; align-items: center; justify-content: center; margin-bottom: 26px;
  border-radius: 13px; color: white; box-shadow: 0 9px 20px rgba(15, 23, 42, .13);
}
.feature-card-icon-blue { background: linear-gradient(135deg, #3b82f6, #2563eb); }
.feature-card-icon-teal { background: linear-gradient(135deg, #18bbaa, #0d9488); }
.feature-card-icon-violet { background: linear-gradient(135deg, #a855f7, #7c3aed); }
.feature-card h2 { margin: 0 0 10px; color: #111827; font-size: 18px; font-weight: 700; }
.feature-card p { margin: 0; color: #677386; font-size: 14px; line-height: 1.8; }
.providers-section { padding: 72px 0 12px; text-align: center; }
.providers-heading h2 { margin: 0 0 10px; color: #111827; font-size: 28px; font-weight: 740; letter-spacing: -.02em; }
.providers-heading p { margin: 0; color: #718096; font-size: 14px; }
.provider-list { display: flex; flex-wrap: wrap; justify-content: center; gap: 12px; margin-top: 36px; }
.provider-chip {
  display: flex; min-height: 56px; align-items: center; gap: 9px; border: 1px solid rgba(45, 212, 191, .55);
  border-radius: 13px; padding: 9px 16px; background: rgba(255, 255, 255, .86); box-shadow: 0 8px 22px rgba(15, 23, 42, .04);
}
.provider-chip-muted { border-color: rgba(148, 163, 184, .2); opacity: .66; }
.provider-mark {
  display: inline-flex; width: 34px; height: 34px; align-items: center; justify-content: center; border-radius: 9px;
  color: white; font-size: 12px; font-weight: 750;
}
.provider-mark-orange { background: linear-gradient(135deg, #fb923c, #f97316); }
.provider-mark-green { background: linear-gradient(135deg, #22c55e, #16a34a); }
.provider-mark-blue { background: linear-gradient(135deg, #3b82f6, #2563eb); }
.provider-mark-rose { background: linear-gradient(135deg, #f43f5e, #db2777); }
.provider-mark-gray { background: linear-gradient(135deg, #9ca3af, #6b7280); }
.provider-name { color: #3d495c; font-size: 13px; font-weight: 650; }
.provider-status { border-radius: 5px; padding: 2px 6px; color: #0f766e; background: #ccfbf1; font-size: 10px; font-weight: 650; }
.provider-status-muted { color: #64748b; background: #f1f5f9; }
.home-footer { position: relative; z-index: 10; border-top: 1px solid rgba(148, 163, 184, .13); padding: 24px; }
.home-footer-inner { display: flex; align-items: center; justify-content: center; gap: 24px; color: #8792a5; font-size: 12px; }
.home-footer-links { display: flex; align-items: center; gap: 16px; }
.home-footer-link { transition: color 150ms ease; } .home-footer-link:hover { color: #0f766e; }

:global(.dark) .public-home { color: #f8fafc; background-color: #07101e; }
:global(.dark) .home-backdrop {
  background:
    radial-gradient(circle at 98% 10%, rgba(20, 184, 166, .14), transparent 27rem),
    radial-gradient(circle at 8% 88%, rgba(20, 184, 166, .08), transparent 31rem),
    linear-gradient(rgba(148, 163, 184, .055) 1px, transparent 1px),
    linear-gradient(90deg, rgba(148, 163, 184, .055) 1px, transparent 1px);
  background-size: auto, auto, 48px 48px, 48px 48px;
}
:global(.dark) .home-logo-frame, :global(.dark) .home-session-button { background: #020617; }
:global(.dark) .home-icon-button { color: #94a3b8; }
:global(.dark) .home-icon-button:hover { color: white; background: rgba(30, 41, 59, .8); }
:global(.dark) .hero-title, :global(.dark) .feature-card h2, :global(.dark) .providers-heading h2 { color: #f8fafc; }
:global(.dark) .hero-subtitle, :global(.dark) .feature-card p, :global(.dark) .providers-heading p { color: #94a3b8; }
:global(.dark) .feature-tag, :global(.dark) .feature-card, :global(.dark) .provider-chip {
  border-color: rgba(71, 85, 105, .55); color: #cbd5e1; background: rgba(15, 23, 42, .78);
}
:global(.dark) .provider-name { color: #dbe4ef; }

@media (max-width: 900px) {
  .home-main { padding-top: 20px; }
  .home-hero { grid-template-columns: 1fr; gap: 48px; padding-top: 38px; text-align: center; }
  .hero-title, .hero-subtitle { margin-inline: auto; }
  .terminal-column { justify-content: center; }
  .features-grid { grid-template-columns: 1fr; max-width: 620px; margin-inline: auto; }
}
@media (max-width: 640px) {
  .home-header { padding: 14px 16px; }
  .home-logo-frame { width: 42px; height: 42px; border-radius: 13px; }
  .home-actions { gap: 3px; }
  .home-main { padding: 24px 16px 52px; }
  .home-hero { padding-bottom: 52px; }
  .hero-title { font-size: clamp(2.6rem, 15vw, 4rem); }
  .hero-subtitle { margin-top: 20px; font-size: 1rem; }
  .terminal-window { min-height: 235px; }
  .terminal-body { padding: 25px 18px; font-size: 11px; }
  .feature-tags { align-items: stretch; gap: 9px; }
  .feature-tag { min-height: 38px; padding: 8px 12px; font-size: 11px; }
  .feature-card { min-height: 0; padding: 24px; }
  .providers-section { padding-top: 56px; }
  .provider-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .provider-chip { min-width: 0; padding: 8px 10px; }
  .provider-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .provider-status { display: none; }
  .home-footer-inner { flex-direction: column; gap: 10px; text-align: center; }
}
@media (prefers-reduced-motion: reduce) {
  .code-line, .cursor { animation: none; opacity: 1; }
  .terminal-window, .feature-card, .hero-cta, .home-session-button, .home-logo-frame { transition: none; }
}

/* Paper dashboard theme */
.public-home {
  color: #302b24;
  background: #f3ecdc;
  font-family: ui-rounded, "Arial Rounded MT Bold", "PingFang SC", "Microsoft YaHei", sans-serif;
}

.home-backdrop {
  background:
    linear-gradient(rgba(205, 188, 139, 0.4) 1px, transparent 1px),
    linear-gradient(90deg, rgba(205, 188, 139, 0.4) 1px, transparent 1px);
  background-size: 36px 36px;
  mask-image: none;
}

.home-header {
  padding: 22px 24px 10px;
}

.home-nav {
  min-height: 62px;
  border: 3px solid #302b24;
  border-radius: 999px;
  padding: 7px 10px 7px 14px;
  background: rgba(250, 246, 235, 0.9);
  box-shadow: 4px 4px 0 rgba(48, 43, 36, 0.16);
}

.home-logo-frame {
  width: 44px;
  height: 44px;
  border: 2px solid #302b24;
  border-radius: 12px;
  background: #faf6eb;
  box-shadow: 2px 2px 0 #302b24;
}

.home-logo:hover .home-logo-frame {
  transform: rotate(-2deg) translateY(-1px);
  box-shadow: 3px 3px 0 #302b24;
}

.home-icon-button {
  color: #302b24;
}

.home-icon-button:hover {
  color: #302b24;
  background: #f4ca61;
}

.home-session-button {
  min-height: 36px;
  border: 2px solid #302b24;
  color: #302b24;
  background: #f4ca61;
  box-shadow: 2px 2px 0 #302b24;
  font-weight: 750;
}

.home-session-button:hover {
  transform: translate(1px, 1px);
  color: #302b24;
  background: #ffd66d;
  box-shadow: 1px 1px 0 #302b24;
}

.home-user-initial {
  border: 1px solid #302b24;
  color: #fff9eb;
  background: #dc492d;
}

.home-main {
  padding: 26px 24px 62px;
}

.home-content {
  position: relative;
  border: 3px solid #302b24;
  border-radius: 24px;
  padding: 34px 48px 48px;
  background: rgba(247, 240, 224, 0.72);
  box-shadow: 7px 7px 0 rgba(48, 43, 36, 0.18);
}

.paper-section-label {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 20px;
  border-bottom: 2px dashed rgba(48, 43, 36, 0.55);
  color: #302b24;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.14em;
}

.paper-section-label span:first-child {
  border: 2px solid #302b24;
  padding: 4px 7px;
  background: #f4ca61;
  line-height: 1;
}

.home-hero {
  min-height: 410px;
  grid-template-columns: minmax(0, 1.05fr) minmax(380px, 0.95fr);
  padding: 50px 0 76px;
}

.hero-copy::before {
  display: inline-block;
  margin-bottom: 18px;
  border: 2px solid #302b24;
  border-radius: 999px;
  padding: 5px 12px;
  content: "DATA · FORWARD";
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.14em;
  transform: rotate(1deg);
}

.hero-title {
  color: #28231d;
  font-family: Georgia, "Times New Roman", "Songti SC", "Noto Serif SC", serif;
  font-size: clamp(3.25rem, 6vw, 5.5rem);
  font-style: italic;
  font-weight: 800;
  letter-spacing: -0.06em;
  line-height: 0.95;
  text-shadow: 2px 2px 0 rgba(220, 73, 45, 0.12);
}

.hero-subtitle {
  color: #514a40;
  font-weight: 650;
}

.hero-cta {
  min-height: 50px;
  border: 3px solid #302b24;
  border-radius: 10px;
  color: #302b24;
  background: #f4ca61;
  box-shadow: 5px 5px 0 #302b24;
  font-weight: 800;
}

.hero-cta:hover {
  transform: translate(2px, 2px) rotate(-1deg);
  box-shadow: 3px 3px 0 #302b24;
}

.terminal-column {
  position: relative;
}

.terminal-container {
  width: min(100%, 450px);
}

.terminal-container::after {
  display: none;
}

.terminal-window {
  min-height: 270px;
  border: 3px solid #302b24;
  border-radius: 16px;
  background: #fbf7ed;
  box-shadow: 7px 7px 0 rgba(48, 43, 36, 0.86);
  transform: rotate(0.7deg);
}

.terminal-window:hover {
  transform: rotate(0deg) translateY(-3px);
  box-shadow: 9px 10px 0 rgba(48, 43, 36, 0.86);
}

.terminal-header {
  border-bottom: 3px solid #302b24;
  background: #f3ead7;
}

.terminal-buttons span {
  border: 1.5px solid #302b24;
  background: transparent;
}

.terminal-title {
  color: #514a40;
  font-weight: 700;
}

.terminal-body {
  color: #302b24;
}

.code-prompt,
.code-success {
  color: #dc492d;
}

.code-cmd,
.code-url {
  color: #2f7294;
}

.code-flag {
  color: #8c5d25;
}

.code-comment {
  color: #766d5e;
}

.code-success {
  border: 1px solid #302b24;
  background: #fff09b;
}

.code-response {
  color: #302b24;
}

.cursor {
  background: #dc492d;
}

.paper-note {
  position: absolute;
  right: -22px;
  top: -52px;
  z-index: 4;
  width: 190px;
  padding: 18px 16px;
  color: #302b24;
  background: #fff09b;
  box-shadow: 5px 7px 0 rgba(48, 43, 36, 0.18);
  font-family: Georgia, "Songti SC", serif;
  font-size: 14px;
  font-weight: 750;
  line-height: 1.45;
  transform: rotate(3deg);
}

.paper-note::before {
  position: absolute;
  top: -10px;
  left: 62px;
  width: 64px;
  height: 20px;
  background: rgba(120, 110, 89, 0.22);
  content: "";
  transform: rotate(-2deg);
}

.feature-tags {
  margin-bottom: 48px;
}

.feature-tag {
  border: 2px solid #302b24;
  border-radius: 999px;
  color: #302b24;
  background: #fbf7ed;
  box-shadow: 2px 2px 0 #302b24;
}

.feature-tag:nth-child(2) {
  background: #fff09b;
  transform: rotate(-1deg);
}

.feature-tag svg {
  color: #dc492d;
}

.features-grid {
  gap: 24px;
}

.feature-card {
  min-height: 230px;
  border: 3px solid #302b24;
  border-radius: 14px;
  background: #fbf7ed;
  box-shadow: 5px 5px 0 rgba(48, 43, 36, 0.86);
}

.feature-card:nth-child(1) { transform: rotate(-0.5deg); }
.feature-card:nth-child(2) { transform: rotate(0.4deg); }
.feature-card:nth-child(3) { transform: rotate(-0.25deg); }

.feature-card:hover {
  transform: translateY(-3px) rotate(0deg);
  border-color: #302b24;
  box-shadow: 7px 8px 0 rgba(48, 43, 36, 0.86);
}

.feature-card-icon {
  border: 2px solid #302b24;
  border-radius: 8px;
  color: #302b24;
  box-shadow: 2px 2px 0 #302b24;
}

.feature-card-icon-blue { background: #8fc4d7; }
.feature-card-icon-teal { background: #f4ca61; }
.feature-card-icon-violet { background: #f4a18e; }

.feature-card h2,
.providers-heading h2 {
  color: #302b24;
  font-family: Georgia, "Times New Roman", "Songti SC", "Noto Serif SC", serif;
  font-weight: 800;
}

.feature-card p,
.providers-heading p {
  color: #5e564a;
}

.providers-section {
  margin-top: 68px;
  border-top: 3px dashed #302b24;
  padding-top: 54px;
}

.providers-heading h2 {
  display: inline-block;
  border-bottom: 5px double #302b24;
  padding: 0 10px 5px;
  font-size: 30px;
  font-style: italic;
}

.provider-chip {
  border: 2px solid #302b24;
  border-radius: 10px;
  background: #fbf7ed;
  box-shadow: 3px 3px 0 #302b24;
}

.provider-mark {
  border: 1.5px solid #302b24;
  border-radius: 6px;
  color: #302b24;
}

.provider-mark-orange { background: #f4a18e; }
.provider-mark-green { background: #a7d1b5; }
.provider-mark-blue { background: #8fc4d7; }
.provider-mark-rose { background: #fff09b; }
.provider-mark-gray { background: #d8d0bf; }

.provider-name {
  color: #302b24;
  font-weight: 800;
}

.provider-status {
  border: 1px solid #302b24;
  color: #302b24;
  background: #fff09b;
  transform: rotate(-2deg);
}

.home-footer {
  border-top: 0;
  color: #5e564a;
}

:global(.dark) .public-home {
  color: #eee5d3;
  background: #24211c;
}

:global(.dark) .home-backdrop {
  background:
    linear-gradient(rgba(143, 129, 91, 0.28) 1px, transparent 1px),
    linear-gradient(90deg, rgba(143, 129, 91, 0.28) 1px, transparent 1px);
  background-size: 36px 36px;
}

:global(.dark) .home-nav,
:global(.dark) .home-content,
:global(.dark) .terminal-window,
:global(.dark) .terminal-header,
:global(.dark) .feature-tag,
:global(.dark) .feature-card,
:global(.dark) .provider-chip {
  border-color: #eee5d3;
  color: #eee5d3;
  background: #302c25;
  box-shadow: 4px 4px 0 rgba(238, 229, 211, 0.42);
}

:global(.dark) .hero-title,
:global(.dark) .hero-subtitle,
:global(.dark) .feature-card h2,
:global(.dark) .feature-card p,
:global(.dark) .providers-heading h2,
:global(.dark) .providers-heading p,
:global(.dark) .provider-name {
  color: #eee5d3;
}

:global(.dark) .home-icon-button {
  color: #eee5d3;
}

@media (max-width: 900px) {
  .home-content {
    padding: 28px 32px 38px;
  }

  .home-hero {
    grid-template-columns: minmax(0, 1fr);
    gap: 48px;
    text-align: center;
  }

  .hero-title,
  .hero-subtitle {
    margin-inline: auto;
  }

  .paper-note {
    position: relative;
    right: auto;
    top: auto;
    width: min(70%, 230px);
    margin: -8px 24px 0 auto;
  }

  .terminal-column {
    align-items: center;
    flex-direction: column;
    justify-content: center;
    width: 100%;
  }

  .providers-section {
    margin-top: 54px;
  }
}

@media (max-width: 640px) {
  .home-header {
    padding: 12px 10px 6px;
  }

  .home-nav {
    min-height: 54px;
    border-width: 2px;
    padding: 5px 6px 5px 9px;
    box-shadow: 3px 3px 0 rgba(48, 43, 36, 0.18);
  }

  .home-logo-frame {
    width: 38px;
    height: 38px;
    border-width: 1.5px;
  }

  .home-main {
    padding: 16px 10px 42px;
  }

  .home-content {
    border-width: 2px;
    border-radius: 16px;
    padding: 20px 14px 30px;
    box-shadow: 4px 4px 0 rgba(48, 43, 36, 0.18);
  }

  .paper-section-label {
    gap: 8px;
    padding-bottom: 14px;
    font-size: 9px;
    letter-spacing: 0.08em;
  }

  .home-hero {
    padding-top: 36px;
  }

  .hero-copy::before {
    margin-bottom: 16px;
  }

  .hero-title {
    font-size: clamp(2.7rem, 14vw, 4rem);
  }

  .terminal-window {
    border-width: 2px;
    box-shadow: 4px 4px 0 rgba(48, 43, 36, 0.86);
    transform: none;
  }

  .terminal-header {
    border-bottom-width: 2px;
  }

  .paper-note {
    width: 72%;
    margin-right: 8px;
    padding: 14px;
    font-size: 12px;
  }

  .feature-card {
    border-width: 2px;
    box-shadow: 4px 4px 0 rgba(48, 43, 36, 0.86);
    transform: none !important;
  }

  .providers-section {
    border-top-width: 2px;
    padding-top: 42px;
  }

  .provider-chip {
    border-width: 1.5px;
    box-shadow: 2px 2px 0 #302b24;
  }
}
</style>
