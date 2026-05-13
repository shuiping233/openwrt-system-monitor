<script setup lang="ts">
import { reactive } from 'vue';
import type { DynamicApiResponse, StaticApiResponse } from '../model';
import { formatMetric, formatValue } from '../utils/convert';

// 定义组件接收的 props
interface Props {
    data: {
        dynamic: DynamicApiResponse;
        static: StaticApiResponse;
    };
}

const props = defineProps<Props>();

// 定义折叠面板状态
const uiState = reactive({
    accordions: {
        storage: true,
        cpu: true,
        network: true,
        sysinfo: true,
    }
});
</script>

<template>
    <!-- Tab: System Overview -->
    <div class="system-overview">
        <!-- 1. Summary Cards -->
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-5 mb-10">
            <div v-if="data.dynamic.cpu?.total?.usage"
                class="bg-slate-800 border border-slate-700 rounded-xl p-5 transition-all hover:-translate-y-0.5 hover:shadow-xl hover:border-slate-500">
                <div class="text-slate-400 text-sm flex justify-between">CPU 总使用率
                    <span class="text-slate-400 text-sm text-right">CPU 温度</span>
                </div>
                <div class="flex justify-between items-baseline mt-1">
                    <!-- 左侧：CPU使用率百分比 -->
                    <div class="text-xl font-bold">
                        {{ formatMetric(data.dynamic.cpu.total.usage.value, data.dynamic.cpu.total.usage.unit) }}
                    </div>

                    <!-- 右侧：CPU温度 -->
                    <div v-if="data.dynamic.cpu?.total?.temperature" class="text-right">
                        <div class="text-xl font-bold">
                            <span :class="{
                                'text-white': data.dynamic.cpu.total.temperature.value < 65,
                                'text-orange-400': data.dynamic.cpu.total.temperature.value >= 65 && data.dynamic.cpu.total.temperature.value < 80,
                                'text-red-500': data.dynamic.cpu.total.temperature.value >= 80
                            }">
                                {{ formatValue(data.dynamic.cpu.total.temperature.value, data.dynamic.cpu.total.temperature.unit) }}
                            </span>
                            <span class="text-slate-400 text-sm ml-1">{{ data.dynamic.cpu.total.temperature.unit }}</span>
                        </div>
                    </div>
                </div>

                <!-- CPU 使用率进度条 -->
                <div class="h-1 bg-slate-700 mt-3 rounded-full overflow-hidden">
                    <div class="h-full bg-violet-500 transition-all duration-500"
                        :style="{ width: data.dynamic.cpu.total.usage.value + '%' }"></div>
                </div>
            </div>

            <!-- Memory -->
            <div v-if="data.dynamic.memory?.used_percent"
                class="bg-slate-800 border border-slate-700 rounded-xl p-5 transition-all hover:-translate-y-0.5 hover:shadow-xl hover:border-slate-500">
                <div class="text-slate-400 text-sm mb-1">内存使用率</div>

                <div class="flex justify-between items-baseline mt-1">
                    <!-- 左侧：百分比 -->
                    <div class="text-xl font-bold">
                        {{ formatMetric(data.dynamic.memory.used_percent.value, data.dynamic.memory.used_percent.unit) }}
                    </div>

                    <!-- 右侧：具体使用量 -->
                    <div class="text-right">
                        <span class="font-bold">{{ formatMetric(data.dynamic.memory.used.value,
                            data.dynamic.memory.used.unit) }}</span>
                        <span class="text-slate-400 mx-1">/</span>
                        <span class="font-bold">{{ formatMetric(data.dynamic.memory.total.value,
                            data.dynamic.memory.total.unit) }}</span>
                    </div>
                </div>

                <div class="h-1 bg-slate-700 mt-3 rounded-full overflow-hidden">
                    <div class="h-full bg-blue-500 transition-all duration-500"
                        :style="{ width: data.dynamic.memory.used_percent.value + '%' }"></div>
                </div>
            </div>

            <!-- Network In -->
            <div v-if="data.dynamic.network?.['pppoe-wan']?.incoming || data.dynamic.network?.['pppoe-wan']?.outgoing"
                class="bg-slate-800 border border-slate-700 rounded-xl p-5">
                <div class="flex flex-col">
                    <div class="text-slate-400 text-sm mb-2">网络流量 (pppoe-wan)</div>

                    <!-- 上行 -->
                    <div v-if="data.dynamic.network?.['pppoe-wan']?.outgoing" class="flex items-center justify-between">
                        <div class="text-orange-500">↑ 上行</div>
                        <div class="text-xl font-bold font-mono text-orange-500">
                            {{ formatMetric(data.dynamic.network['pppoe-wan'].outgoing.value,
                                data.dynamic.network['pppoe-wan'].outgoing.unit) }}
                        </div>
                    </div>

                    <!-- 下行 -->
                    <div v-if="data.dynamic.network?.['pppoe-wan']?.incoming"
                        class="flex items-center justify-between mb-1">
                        <div class="text-cyan-500">↓ 下行</div>
                        <div class="text-xl font-bold font-mono text-cyan-500">
                            {{ formatMetric(data.dynamic.network['pppoe-wan'].incoming.value,
                                data.dynamic.network['pppoe-wan'].incoming.unit) }}
                        </div>
                    </div>
                </div>
            </div>

            <div v-if="data.dynamic.network?.['pppoe-wan']?.incoming || data.dynamic.network?.['pppoe-wan']?.outgoing"
                class="bg-slate-800 border border-slate-700 rounded-xl p-5">
                <div class="text-slate-400 text-sm mb-2">总网卡流量</div>
                <!-- 上行 -->
                <div v-if="data.dynamic.network?.['pppoe-wan']?.outgoing" class="flex items-center justify-between">
                    <div class="text-orange-500">↑ 上行</div>
                    <div class="text-xl font-bold font-mono text-orange-500">
                        {{ formatMetric(data.dynamic.network.total.incoming.value,
                            data.dynamic.network.total.incoming.unit) }}
                    </div>
                </div>

                <!-- 下行 -->
                <div v-if="data.dynamic.network?.['pppoe-wan']?.incoming"
                    class="flex items-center justify-between mb-1">
                    <div class="text-cyan-500">↓ 下行</div>
                    <div class="text-xl font-bold font-mono text-cyan-500">
                        {{ formatMetric(data.dynamic.network.total.outgoing.value,
                            data.dynamic.network.total.outgoing.unit) }}
                    </div>
                </div>
            </div>

            <!-- System Info (Smaller cards) -->
            <div v-if="data.dynamic.system?.uptime"
                class="bg-slate-800 border border-slate-700 rounded-xl p-5 flex items-center justify-between">
                <div class="text-slate-400 text-sm">运行时间</div>
                <div class="text-lg font-bold">{{ data.dynamic.system.uptime }}</div>
            </div>
            <div v-if="data.static.system?.hostname"
                class="bg-slate-800 border border-slate-700 rounded-xl p-5 flex items-center justify-between">
                <div class="text-slate-400 text-sm">主机名</div>
                <div class="text-lg font-bold font-mono">{{ data.static.system.hostname }}</div>
            </div>
        </div>

        <!-- 2. Detailed Sections (Accordions with Cards) -->

        <!-- Storage -->
        <div v-if="data.dynamic.storage">
            <div @click="uiState.accordions.storage = !uiState.accordions.storage"
                class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
                <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">存储详情</h3>
                <span class="text-slate-500 transition-transform duration-300"
                    :class="{ 'rotate-180': uiState.accordions.storage }">▼</span>
            </div>
            <div v-show="uiState.accordions.storage" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">
                <div v-for="(dev, name) in data.dynamic.storage" :key="name"
                    class="bg-slate-800 border border-slate-700 rounded-xl p-5 transition-all hover:-translate-y-0.5 hover:shadow-xl">
                    <div class="flex justify-between items-center mb-4">
                        <span class="text-xl font-bold">{{ name }}</span>
                        <span class="bg-slate-700 px-2 py-0.5 rounded text-xs font-mono text-slate-300">{{
                            formatValue(dev.used_percent.value, dev.used_percent.unit) }}%</span>
                    </div>
                    <div class="grid grid-cols-3 gap-2 text-sm mb-3">
                        <div><span class="text-slate-500">读:</span> {{ formatValue(dev.read.value, dev.read.unit) }} <span
                                class="font-mono text-slate-200">
                                {{ dev.read.unit }}</span></div>
                        <div><span class="text-slate-500">写:</span> {{ formatValue(dev.write.value, dev.write.unit) }} <span
                                class="font-mono text-slate-200">
                                {{ dev.write.unit }}</span></div>
                        <div><span class="text-slate-500"></span></div>
                        <div><span class="text-slate-500">使用量:</span> <span class="font-mono">{{
                            formatMetric(dev.used_percent.value, dev.used_percent.unit) }}</span></div>
                        <div><span class="text-slate-500">总容量:</span> <span class="font-mono">{{
                            formatMetric(dev.total.value, dev.total.unit) }}</span></div>
                        <div><span class="text-slate-500">已用:</span> <span class="font-mono">{{
                            formatMetric(dev.used.value, dev.used.unit)
                                }}</span></div>
                    </div>
                    <div class="h-1.5 bg-slate-900 rounded-full overflow-hidden mt-2">
                        <div class="h-full bg-cyan-500 transition-all duration-500"
                            :style="{ width: Math.min(dev.used_percent.value, 100) + '%' }"></div>
                    </div>
                </div>
            </div>
        </div>

        <!-- CPU -->
        <div v-if="data.dynamic.cpu" class="mt-8">
            <div @click="uiState.accordions.cpu = !uiState.accordions.cpu"
                class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
                <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">CPU 核心详情</h3>
                <span class="text-slate-500 transition-transform duration-300"
                    :class="{ 'rotate-180': uiState.accordions.cpu }">▼</span>
            </div>
            <div v-show="uiState.accordions.cpu" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-5">
                <div v-for="(core, name) in data.dynamic.cpu" :key="name"
                    class="bg-slate-800 border border-slate-700 rounded-xl p-5 transition-all hover:-translate-y-0.5 hover:shadow-xl">
                    <div class="flex justify-between mb-2">
                        <span class="text-lg font-bold">{{ name }}</span>
                        <span class="text-lg font-bold">{{ formatValue(core.usage.value, core.usage.unit) }}%</span>
                    </div>
                    <div class="h-1.5 bg-slate-900 rounded-full overflow-hidden mb-2">
                        <div class="h-full bg-violet-500 transition-all duration-500"
                            :style="{ width: Math.min(core.usage.value, 100) + '%' }"></div>
                    </div>
                    <div v-if="core.temperature.value > 0" class="text-xs text-slate-500 mt-2">温度: {{
                        formatValue(core.temperature.value, core.temperature.unit) }}°C</div>
                </div>
            </div>
        </div>

        <!-- Network Interfaces -->
        <div v-if="data.dynamic.network || data.static.network" class="mt-8">
            <div @click="uiState.accordions.network = !uiState.accordions.network"
                class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
                <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">网络配置详情</h3>
                <span class="text-slate-500 transition-transform duration-300"
                    :class="{ 'rotate-180': uiState.accordions.network }">▼</span>
            </div>
            <div v-show="uiState.accordions.network" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5">

                <!-- total netowrk device io -->
                <div v-if="data.dynamic.network?.total"
                    class="bg-slate-800 border border-slate-700 rounded-xl p-5 transition-all hover:-translate-y-0.5 hover:shadow-xl">
                    <h3 class="text-lg font-bold mb-4">总网卡流量</h3>
                    <div class="flex justify-between items-center">
                        <div class="text-xl font-bold font-mono text-cyan-500">↓ {{
                            formatMetric(data.dynamic.network.total.incoming.value,
                                data.dynamic.network.total.incoming.unit)
                            }} </div>
                        <div class="text-xl font-bold font-mono text-orange-500">↑ {{
                            formatMetric(data.dynamic.network.total.outgoing.value,
                                data.dynamic.network.total.outgoing.unit)
                            }} </div>
                    </div>
                </div>

                <!-- IO Cards -->
                <template v-for="(net, iface) in data.dynamic.network" :key="'io-'+iface">
                    <div v-if="iface !== 'total'"
                        class="bg-slate-800 border border-slate-700 rounded-xl p-5 transition-all hover:-translate-y-0.5 hover:shadow-xl">
                        <h3 class="text-lg font-bold mb-4">{{ iface }} <span
                                class="text-slate-500 text-sm font-normal">IO</span>
                        </h3>
                        <div class="flex justify-between items-center">
                            <div class="text-xl font-bold font-mono text-cyan-500">↓ {{ formatMetric(net.incoming.value,
                                net.incoming.unit)
                                }} </div>
                            <div class="text-xl font-bold font-mono text-orange-500">↑ {{ formatMetric(net.outgoing.value,
                                net.outgoing.unit)
                                }} </div>
                        </div>
                    </div>
                </template>
                <!-- IP Cards -->
                <template v-for="(info, iface) in data.static.network" :key="'ip-' + iface">
                    <div v-if="iface !== 'global' && iface !== 'lo'"
                        class="bg-slate-800 border border-slate-700 rounded-xl p-5 transition-all hover:-translate-y-0.5 hover:shadow-xl">
                        <h3 class="text-lg font-bold mb-3">{{ iface }} <span
                                class="text-slate-500 text-sm font-normal">IP</span>
                        </h3>
                        <div class="text-sm font-mono wrap-break-word space-y-1">
                            <div v-for="ip in info.ipv4" :key="ip" class="text-slate-200">{{ ip }}</div>
                            <div v-for="ip in info.ipv6" :key="ip" class="text-slate-200 text-xs">{{ ip }}</div>
                        </div>
                    </div>
                </template>
                <!-- Gateway -->
                <div v-if="data.static.network?.global?.gateway && data.static.network.global.gateway !== 'unknown'"
                    class="bg-slate-800 border border-slate-700 rounded-xl p-5 flex flex-col justify-center">
                    <h3 class="text-slate-400 text-sm mb-2">网关</h3>
                    <div class="text-2xl font-bold font-mono">{{ data.static.network.global.gateway }}</div>
                </div>
                <!-- DNS -->
                <div v-if="data.static.network?.global?.dns && data.static.network.global.dns.length > 0"
                    class="bg-slate-800 border border-slate-700 rounded-xl p-5 flex flex-col justify-center">
                    <h3 class="text-slate-400 text-sm mb-2">DNS</h3>
                    <template v-for="dns in data.static.network.global.dns" :key="dns">
                        <div class="text-2xl font-bold font-mono">{{ dns }}</div>
                    </template>
                </div>
            </div>
        </div>

        <!-- System Info -->
        <div v-if="data.static.system" class="mt-8">
            <div @click="uiState.accordions.sysinfo = !uiState.accordions.sysinfo"
                class="py-2.5 border-b border-slate-700 mb-5 cursor-pointer select-none flex justify-between items-center group">
                <h3 class="text-lg font-semibold text-slate-200 group-hover:text-white">系统信息详情</h3>
                <span class="text-slate-500 transition-transform duration-300"
                    :class="{ 'rotate-180': uiState.accordions.sysinfo }">▼</span>
            </div>
            <div v-show="uiState.accordions.sysinfo" class="bg-slate-800 border border-slate-700 rounded-xl p-6">
                <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-6">
                    <div><span class="text-slate-500 block text-sm mb-1">OS:</span> <span class="font-medium">{{
                        data.static.system.os
                            }}</span></div>
                    <div><span class="text-slate-500 block text-sm mb-1">Hostname:</span> <span class="font-medium">{{
                        data.static.system.hostname }}</span></div>
                    <div><span class="text-slate-500 block text-sm mb-1">Kernel:</span> <span class="font-medium">{{
                        data.static.system.kernel }}</span></div>
                    <div><span class="text-slate-500 block text-sm mb-1">Device:</span> <span class="font-medium">{{
                        data.static.system.device_name }}</span></div>
                    <div><span class="text-slate-500 block text-sm mb-1">Arch:</span> <span class="font-medium">{{
                        data.static.system.arch }}</span></div>
                    <div><span class="text-slate-500 block text-sm mb-1">Timezone:</span> <span class="font-medium">{{
                        data.static.system.timezone }}</span></div>
                </div>
            </div>
        </div>
    </div>
</template>


<style></style>