<script setup lang="ts">
import { computed, onUnmounted, shallowRef } from "vue";

defineProps<{
  text: string;
  focusable?: boolean;
}>();

const visible = shallowRef(false);
const position = shallowRef({ left: 0, top: 0 });
const placement = shallowRef<"top" | "bottom">("top");

const tooltipStyle = computed(() => ({
  left: `${position.value.left}px`,
  top: `${position.value.top}px`,
}));

function show(event: MouseEvent | FocusEvent) {
  const target = event.currentTarget as HTMLElement | null;
  if (!target) return;
  const rect = target.getBoundingClientRect();
  placement.value = rect.top >= 48 ? "top" : "bottom";
  position.value = {
    left: Math.min(Math.max(rect.left + rect.width / 2, 150), window.innerWidth - 150),
    top: placement.value === "top" ? rect.top - 8 : rect.bottom + 8,
  };
	visible.value = true;
	window.addEventListener("scroll", hideOnScroll, true);
}

function hide() {
	visible.value = false;
	window.removeEventListener("scroll", hideOnScroll, true);
}

function hideOnScroll() {
  hide();
}

onUnmounted(() => window.removeEventListener("scroll", hideOnScroll, true));
</script>

<template>
  <span
    class="inline-flex"
    :tabindex="focusable === false ? undefined : 0"
    :aria-label="text"
    @mouseenter="show"
    @mouseleave="hide"
    @focus="show"
    @blur="hide"
  >
    <slot />
  </span>
  <Teleport to="body">
    <div
      v-if="visible"
      role="tooltip"
      class="pointer-events-none fixed z-[70] max-w-[280px] -translate-x-1/2 rounded-md bg-gray-950 px-2.5 py-1.5 text-[11px] leading-4 text-white shadow-lg dark:bg-white dark:text-gray-950"
      :class="placement === 'top' ? '-translate-y-full' : ''"
      :style="tooltipStyle"
    >
      {{ text }}
    </div>
  </Teleport>
</template>
