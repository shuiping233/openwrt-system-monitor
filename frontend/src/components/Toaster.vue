<script setup lang="ts">
import { useToast } from "../useToast";

// 获取 Toast 列表
const { list, removeToast } = useToast();
</script>

<template>
  <div class="fixed bottom-5 right-5 z-50 flex flex-col gap-3 pointer-events-none">
    <TransitionGroup name="toast" tag="div" class="flex flex-col gap-3">
      <div v-for="item in list" :key="item.id" :class="[
        'pointer-events-auto bg-slate-800 border border-slate-700 shadow-lg rounded-lg p-4 flex items-center min-w-75 max-w-md cursor-pointer transition-all',
        {
          'border-l-4 border-l-green-500': item.type === 'success',
          'border-l-4 border-l-red-500': item.type === 'error',
        },
      ]" @click="removeToast(item.id)">
        <!-- 图标 -->
        <div class="mr-3 shrink-0">
          <svg v-if="item.type === 'success'" class="w-5 h-5 text-green-500" fill="none" stroke="currentColor"
            viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
          </svg>
          <svg v-else class="w-5 h-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
          </svg>
        </div>

        <!-- 文本 -->
        <div class="flex-1">
          <p :class="['text-sm font-medium', item.type === 'success' ? 'text-white' : 'text-white']">
            {{ item.message }}
          </p>
        </div>

        <!-- 关闭按钮 -->
        <button @click.stop="removeToast(item.id)" class="ml-2 text-slate-400 hover:text-white">
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
          </svg>
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style>
/* Toast 进入和离开动画 */
.toast-enter-active,
.toast-leave-active {
  transition: all 0.3s ease;
}

.toast-enter-from {
  opacity: 0;
  transform: translateX(30px);
}

.toast-leave-to {
  opacity: 0;
  transform: translateX(30px);
}

.toast-leave-active {
  position: absolute;
  /* 确保离开时不占位 */
  width: 100%;
  right: 0;
}
</style>
