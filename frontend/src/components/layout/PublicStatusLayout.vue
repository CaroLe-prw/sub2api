<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <PlazaNavBar login-redirect="/monitor" />
    <main class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import PlazaNavBar from '@/components/modelPlaza/PlazaNavBar.vue'
</script>
