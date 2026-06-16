<script setup lang="ts">
import { computed } from "vue";

interface Props {
  // 分页大小
  pageSize: number;
  // 分页大小选项（可选，有默认值 [20, 50, 100, 500, 1000]）
  pageSizeOptions?: number[];
  // 是否自定义分页大小
  isCustomPageSize: boolean;
  // 自定义分页大小输入值
  customPageSize: string;
  // 页码输入值
  pageInputValue: string;
  // 当前页码（从0开始） 
  currentPageIndex: number;
  // 总页数
  pageCount: number;
  // 总行数
  totalRows: number;
  // 是否能上一页
  canPreviousPage: boolean;
  // 是否能下一页
  canNextPage: boolean;
}

const props = defineProps<Props>();

// 默认分页大小选项（连接列表使用）
const defaultPageSizeOptions = [20, 50, 100, 500, 1000];

// 实际使用的分页大小选项（使用传入值或默认值）
const effectivePageSizeOptions = computed(() => props.pageSizeOptions ?? defaultPageSizeOptions);

const emit = defineEmits<{
  "update:pageSize": [value: number];
  "update:isCustomPageSize": [value: boolean];
  "update:customPageSize": [value: string];
  "update:pageInputValue": [value: string];
  switchToPresetSize: [size: number];
  handleCustomPageSizeChange: [];
  jumpToPage: [];
  setPageIndex: [index: number];
  previousPage: [];
  nextPage: [];
}>();

// 处理预设分页大小切换
const onSwitchToPresetSize = (size: number) => {
  emit("switchToPresetSize", size);
};

// 处理自定义分页大小变更
const onHandleCustomPageSizeChange = () => {
  emit("handleCustomPageSizeChange");
};

// 处理页码跳转
const onJumpToPage = () => {
  emit("jumpToPage");
};

// 处理设置页码
const onSetPageIndex = (index: number) => {
  emit("setPageIndex", index);
};

// 处理上一页
const onPreviousPage = () => {
  emit("previousPage");
};

// 处理下一页
const onNextPage = () => {
  emit("nextPage");
};

// 当前显示页码（从1开始）
const displayPageIndex = computed(() => props.currentPageIndex + 1);

// 根据 pageSizeOptions 动态计算分组（用于不同布局）
// 手机端和平板端：每行2个按钮
const pageSizePairs = computed(() => {
  const options = effectivePageSizeOptions.value;
  const pairs: number[][] = [];
  for (let i = 0; i < options.length; i += 2) {
    pairs.push(options.slice(i, i + 2));
  }
  return pairs;
});
</script>

<template>
  <div class="border-t border-slate-700 bg-slate-800/50">
    <!-- 手机端（<= 450px）-->
    <div class="px-4 py-4 flex flex-col gap-6 [@media(min-width:451px)]:hidden">
      <!-- 分页大小控件：完全竖向分组排列 -->
      <div class="flex flex-col gap-3">
        <span class="text-xs text-slate-400">每页显示：</span>
        <div class="flex flex-col gap-2">
          <div v-for="(pair, pairIndex) in pageSizePairs" :key="pairIndex" class="flex gap-2">
            <button v-for="size in pair" :key="size" @click="onSwitchToPresetSize(size)"
              class="flex-1 text-xs py-2 rounded border border-slate-600 transition-colors" :class="[
                !isCustomPageSize && pageSize === size
                  ? 'bg-blue-600 border-blue-600 text-white'
                  : 'bg-slate-700 text-slate-300',
              ]">
              {{ size }} 条
            </button>
            <!-- 如果是最后一行且按钮数量为奇数，填充自定义输入框 -->
            <template v-if="pairIndex === pageSizePairs.length - 1 && pair.length === 1">
              <div class="flex-1 flex items-center gap-1 bg-slate-900 border border-slate-600 rounded px-2">
                <input :value="customPageSize"
                  @input="$emit('update:customPageSize', ($event.target as HTMLInputElement).value)" type="number"
                  placeholder="自定义" class="w-full bg-transparent text-xs py-2 text-white outline-none"
                  @change="onHandleCustomPageSizeChange" />
                <span class="text-[10px] text-slate-500 whitespace-nowrap">条</span>
              </div>
            </template>
          </div>
          <!-- 如果选项数量是偶数，单独一行显示自定义输入 -->
          <div v-if="effectivePageSizeOptions.length % 2 === 0" class="flex gap-2 items-center">
            <div class="flex-1 flex items-center gap-1 bg-slate-900 border border-slate-600 rounded px-2">
              <input :value="customPageSize"
                @input="$emit('update:customPageSize', ($event.target as HTMLInputElement).value)" type="number"
                placeholder="自定义" class="w-full bg-transparent text-xs py-2 text-white outline-none"
                @change="onHandleCustomPageSizeChange" />
              <span class="text-[10px] text-slate-500 whitespace-nowrap">条</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 页码控件：完全竖向分组排列 -->
      <div class="flex flex-col gap-3">
        <div class="flex justify-between items-center">
          <span class="text-xs text-slate-400">第 {{ displayPageIndex }} 页</span>
          <span class="text-xs text-slate-500">共 {{ pageCount }} 页 / {{ totalRows }} 条</span>
        </div>
        <div class="flex flex-col gap-2">
          <div class="flex gap-2">
            <button @click="onSetPageIndex(0)" :disabled="!canPreviousPage"
              class="flex-1 text-xs py-2 rounded bg-slate-700 text-slate-300 disabled:opacity-40 border border-slate-600">
              首页
            </button>
            <button @click="onPreviousPage" :disabled="!canPreviousPage"
              class="flex-1 text-xs py-2 rounded bg-slate-700 text-slate-300 disabled:opacity-40 border border-slate-600">
              上一页
            </button>
          </div>
          <div class="flex items-center gap-2 bg-slate-900 border border-slate-600 rounded px-3 py-1">
            <span class="text-xs text-slate-400">跳转至</span>
            <input :value="pageInputValue"
              @input="$emit('update:pageInputValue', ($event.target as HTMLInputElement).value)" type="number"
              class="flex-1 bg-transparent text-xs text-center text-slate-300 outline-none" @change="onJumpToPage" />
            <span class="text-xs text-slate-400">页</span>
          </div>
          <div class="flex gap-2">
            <button @click="onSetPageIndex(pageCount - 1)" :disabled="!canNextPage"
              class="flex-1 text-xs py-2 rounded bg-slate-700 text-slate-300 disabled:opacity-40 border border-slate-600">
              末页
            </button>
            <button @click="onNextPage" :disabled="!canNextPage"
              class="flex-1 text-xs py-2 rounded bg-slate-700 text-slate-300 disabled:opacity-40 border border-slate-600">
              下一页
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- 平板端（450px - 900px）-->
    <div
      class="px-4 py-4 hidden [@media(min-width:451px)]:flex [@media(min-width:901px)]:hidden items-start justify-between gap-8">
      <!-- 分页大小控件：两个一组竖向分组排列 -->
      <div class="flex flex-col gap-3 flex-1 max-w-60">
        <span class="text-xs text-slate-400 font-medium text-left">每页显示：</span>
        <div class="flex flex-col gap-2">
          <div v-for="(pair, pairIndex) in pageSizePairs" :key="pairIndex" class="flex gap-2">
            <button v-for="size in pair" :key="size" @click="onSwitchToPresetSize(size)"
              class="flex-1 text-xs py-1.5 rounded border border-slate-600 transition-colors" :class="[
                !isCustomPageSize && pageSize === size
                  ? 'bg-blue-600 border-blue-600 text-white'
                  : 'bg-slate-700 text-slate-300 hover:bg-slate-600',
              ]">
              {{ size }}
            </button>
            <!-- 如果是最后一行且按钮数量为奇数，填充自定义输入框 -->
            <template v-if="pairIndex === pageSizePairs.length - 1 && pair.length === 1">
              <input :value="customPageSize"
                @input="$emit('update:customPageSize', ($event.target as HTMLInputElement).value)" type="number"
                placeholder="自定义"
                class="flex-1 min-w-0 bg-slate-900 border border-slate-600 rounded text-xs px-2 py-1.5 text-white outline-none focus:border-blue-400"
                @change="onHandleCustomPageSizeChange" />
            </template>
          </div>
          <!-- 如果选项数量是偶数，单独一行显示自定义输入 -->
          <div v-if="effectivePageSizeOptions.length % 2 === 0" class="flex gap-2">
            <input :value="customPageSize"
              @input="$emit('update:customPageSize', ($event.target as HTMLInputElement).value)" type="number"
              placeholder="自定义"
              class="flex-1 min-w-0 bg-slate-900 border border-slate-600 rounded text-xs px-2 py-1.5 text-white outline-none focus:border-blue-400"
              @change="onHandleCustomPageSizeChange" />
          </div>
        </div>
      </div>
      <!-- 页码控件：两个一组竖向分组排列 -->
      <div class="flex flex-col gap-3 flex-1 max-w-60">
        <span class="text-xs text-slate-400 font-medium text-right">页码导航：</span>
        <div class="flex flex-col gap-2">
          <div class="flex gap-2">
            <button @click="onSetPageIndex(0)" :disabled="!canPreviousPage"
              class="flex-1 text-xs py-1.5 rounded bg-slate-700 text-slate-300 border border-slate-600 disabled:opacity-40">
              首页
            </button>
            <button @click="onPreviousPage" :disabled="!canPreviousPage"
              class="flex-1 text-xs py-1.5 rounded bg-slate-700 text-slate-300 border border-slate-600 disabled:opacity-40">
              上页
            </button>
          </div>
          <div class="flex items-center gap-2 px-2 py-1 bg-slate-900 border border-slate-600 rounded">
            <input :value="pageInputValue"
              @input="$emit('update:pageInputValue', ($event.target as HTMLInputElement).value)" type="number"
              class="w-full bg-transparent text-xs text-center text-white outline-none" @change="onJumpToPage" />
            <span class="text-[10px] text-slate-500 whitespace-nowrap">/ {{ pageCount }}</span>
          </div>
          <div class="flex gap-2">
            <button @click="onSetPageIndex(pageCount - 1)" :disabled="!canNextPage"
              class="flex-1 text-xs py-1.5 rounded bg-slate-700 text-slate-300 border border-slate-600 disabled:opacity-40">
              末页
            </button>
            <button @click="onNextPage" :disabled="!canNextPage"
              class="flex-1 text-xs py-1.5 rounded bg-slate-700 text-slate-300 border border-slate-600 disabled:opacity-40">
              下页
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- pc端（>= 900px）-->
    <div class="px-4 py-3 hidden [@media(min-width:901px)]:flex flex-wrap items-center justify-between gap-3">
      <!-- 分页大小控件：展开成一行横向排列 -->
      <div class="flex items-center gap-2">
        <span class="text-xs text-slate-400">每页显示：</span>
        <button v-for="size in effectivePageSizeOptions" :key="size" @click="onSwitchToPresetSize(size)"
          class="text-xs px-2.5 py-1 rounded border border-slate-600 transition-colors" :class="{
            'bg-blue-600 border-blue-600 text-white': !isCustomPageSize && pageSize === size,
            'bg-slate-700 text-slate-300 hover:bg-slate-600': isCustomPageSize || pageSize !== size,
          }">
          {{ size }}
        </button>
        <div class="flex items-center gap-1 ml-1">
          <input :value="customPageSize"
            @input="$emit('update:customPageSize', ($event.target as HTMLInputElement).value)" type="number"
            class="w-16 text-xs px-2 py-1 rounded bg-slate-900 border border-slate-600 text-white outline-none focus:border-blue-400"
            @change="onHandleCustomPageSizeChange" />
          <span class="text-xs text-slate-400">条</span>
        </div>
      </div>
      <!-- 页码控件：展开成一行横向排列 -->
      <div class="flex items-center gap-4">
        <span class="text-xs text-slate-500 whitespace-nowrap"> 共 {{ totalRows }} 条记录 </span>
        <div class="flex items-center gap-1">
          <button @click="onSetPageIndex(0)" :disabled="!canPreviousPage"
            class="text-xs px-2 py-1 rounded bg-slate-700 text-slate-300 disabled:opacity-50 hover:bg-slate-600 border border-slate-600 transition-colors">
            首页
          </button>
          <button @click="onPreviousPage" :disabled="!canPreviousPage"
            class="text-xs px-2 py-1 rounded bg-slate-700 text-slate-300 disabled:opacity-50 hover:bg-slate-600 border border-slate-600 transition-colors">
            上一页
          </button>

          <div class="flex items-center gap-1 px-3">
            <input :value="pageInputValue"
              @input="$emit('update:pageInputValue', ($event.target as HTMLInputElement).value)" type="number"
              class="w-12 text-xs px-1 py-1 rounded bg-slate-900 border border-slate-600 text-white text-center outline-none focus:border-blue-400"
              @change="onJumpToPage" />
            <span class="text-xs text-slate-400">/ {{ pageCount }}</span>
          </div>

          <button @click="onNextPage" :disabled="!canNextPage"
            class="text-xs px-2 py-1 rounded bg-slate-700 text-slate-300 disabled:opacity-50 hover:bg-slate-600 border border-slate-600 transition-colors">
            下一页
          </button>
          <button @click="onSetPageIndex(pageCount - 1)" :disabled="!canNextPage"
            class="text-xs px-2 py-1 rounded bg-slate-700 text-slate-300 disabled:opacity-50 hover:bg-slate-600 border border-slate-600 transition-colors">
            末页
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
