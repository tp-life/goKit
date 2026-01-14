<template>
  <div class="flex items-center gap-2">
    <div
      :class="[
        'w-2 h-2 rounded-full',
        connected ? 'bg-green-500' : 'bg-red-500'
      ]"
    ></div>
    <span class="text-sm text-slate-300 capitalize">{{ name }}</span>
  </div>
</template>

<script setup>
defineProps({
  name: {
    type: String,
    required: true,
  },
  connected: {
    type: Boolean,
    default: false,
  },
})
</script>
