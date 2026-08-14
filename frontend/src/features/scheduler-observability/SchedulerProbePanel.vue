<script setup lang="ts">
import MonitorAutoModelsPanel from '@/components/admin/monitor/MonitorAutoModelsPanel.vue'
import MonitorDataPanel from '@/components/admin/monitor/MonitorDataPanel.vue'
</script>

<template>
  <div class="space-y-4 sm:space-y-5" data-testid="scheduler-probe-panel">
    <MonitorAutoModelsPanel />
    <MonitorDataPanel />
  </div>
</template>
