<script lang="ts">
    import { onMount, onDestroy } from "svelte";
    import { wsCall } from "$lib/utils/ws_fetch";
    import * as api from "$lib/gen/api_pb.js";
    import { 
        Terminal, 
        Search, 
        Trash2, 
        Pause, 
        Play, 
        Download, 
        Settings, 
        ChevronLeft,
        ShieldAlert,
        Cpu,
        Activity
    } from "@lucide/svelte";
    import { fade } from "svelte/transition";

    let logs = $state<string[]>([]);
    let isPaused = $state(false);
    let filter = $state("");
    let terminalElement = $state<HTMLDivElement | null>(null);
    let autoScroll = $state(true);
    let subscription: any = null;

    $effect(() => {
        if (autoScroll && terminalElement && logs.length > 0) {
            terminalElement.scrollTop = terminalElement.scrollHeight;
        }
    });

    onMount(async () => {
        startStreaming();
    });

    onDestroy(() => {
        stopStreaming();
    });

    async function startStreaming() {
        if (subscription) return;
        
        const iter = wsCall(
            "system::logs",
            api.EmptySchema,
            {},
            [api.SystemLogChunkSchema]
        ).many();

        subscription = iter;

        for await (const [chunk, err] of iter) {
            if (err) {
                console.error("[SYSADMIN] Log stream error:", err);
                break;
            }
            if (chunk && !isPaused) {
                if (chunk.lines && chunk.lines.length > 0) {
                    logs = [...logs, ...chunk.lines].slice(-2000);
                } else if (chunk.line) {
                    logs = [...logs, chunk.line].slice(-2000);
                }
            }
        }
    }

    function stopStreaming() {
        // ws_fetch doesn't explicitly support canceling a generator yet,
        // but it will stop when the session is closed or another request with same ID starts.
        // For now, we rely on the component destruction and the backend checking s.IsClosed().
        subscription = null;
    }

    function clearLogs() {
        logs = [];
    }

    function downloadLogs() {
        const blob = new Blob([logs.join("")], { type: "text/plain" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `wealthengine-logs-${new Date().toISOString()}.txt`;
        a.click();
        URL.revokeObjectURL(url);
    }

    let filteredLogs = $derived(
        filter 
            ? logs.filter(line => line.toLowerCase().includes(filter.toLowerCase()))
            : logs
    );
</script>

<div class="min-h-screen bg-slate-900 pb-12 pt-8 px-4 md:px-8 font-sans">
    <div class="max-w-[1600px] mx-auto space-y-8">
        
        <!-- Header -->
        <header class="flex flex-col lg:flex-row lg:items-end justify-between gap-8">
            <div class="space-y-2">
                <a href="/dashboard" class="flex items-center gap-2 text-slate-400 hover:text-white transition-colors text-xs font-black uppercase tracking-widest mb-4">
                    <ChevronLeft class="w-4 h-4" />
                    Back to Nodes
                </a>
                <h1 class="text-5xl font-black tracking-tight text-white flex items-center gap-4">
                    <ShieldAlert class="w-12 h-12 text-rose-500 animate-pulse" />
                    System <span class="gradient-text">Diagnostics</span>.
                </h1>
                <div class="flex items-center gap-6 mt-4">
                    <div class="flex items-center gap-2 px-3 py-1 bg-slate-800 rounded-lg border border-slate-700">
                        <Activity class="w-4 h-4 text-emerald-500" />
                        <span class="text-[10px] font-black uppercase tracking-widest text-slate-300">Backend Live</span>
                    </div>
                    <div class="flex items-center gap-2 px-3 py-1 bg-slate-800 rounded-lg border border-slate-700">
                        <Cpu class="w-4 h-4 text-indigo-400" />
                        <span class="text-[10px] font-black uppercase tracking-widest text-slate-300">Log Buffer: {logs.length} Lines</span>
                    </div>
                </div>
            </div>

            <div class="flex flex-wrap items-center gap-3">
                <div class="relative group">
                    <Search class="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500 group-focus-within:text-indigo-400 transition-colors" />
                    <input 
                        type="text" 
                        bind:value={filter}
                        placeholder="FILTER LOGS..." 
                        class="pl-12 pr-6 py-3 bg-slate-800 border border-slate-700 text-white rounded-2xl text-[10px] font-black uppercase tracking-widest focus:ring-4 focus:ring-indigo-500/20 focus:border-indigo-500 outline-none w-64 transition-all placeholder:text-slate-600"
                    />
                </div>

                <button 
                    onclick={() => isPaused = !isPaused}
                    class="p-3 bg-slate-800 border border-slate-700 text-slate-300 rounded-2xl hover:bg-slate-700 hover:text-white transition-all shadow-lg"
                    title={isPaused ? "Resume Stream" : "Pause Stream"}
                >
                    {#if isPaused}
                        <Play class="w-5 h-5 text-emerald-400" />
                    {:else}
                        <Pause class="w-5 h-5" />
                    {/if}
                </button>

                <button 
                    onclick={clearLogs}
                    class="p-3 bg-slate-800 border border-slate-700 text-slate-300 rounded-2xl hover:bg-rose-900/40 hover:text-rose-400 hover:border-rose-900/60 transition-all shadow-lg"
                    title="Clear Buffer"
                >
                    <Trash2 class="w-5 h-5" />
                </button>

                <button 
                    onclick={downloadLogs}
                    class="p-3 bg-slate-800 border border-slate-700 text-slate-300 rounded-2xl hover:bg-indigo-900/40 hover:text-indigo-400 hover:border-indigo-900/60 transition-all shadow-lg"
                    title="Download Current Logs"
                >
                    <Download class="w-5 h-5" />
                </button>

                <button 
                    onclick={() => autoScroll = !autoScroll}
                    class="p-3 bg-slate-800 border border-slate-700 text-slate-300 rounded-2xl hover:bg-slate-700 hover:text-white transition-all shadow-lg {autoScroll ? 'ring-2 ring-indigo-500/50' : ''}"
                    title="Toggle Auto-Scroll"
                >
                    <Settings class="w-5 h-5" />
                </button>
            </div>
        </header>

        <!-- Terminal Card -->
        <div class="glass-card overflow-hidden border border-white/5 rounded-[2.5rem] shadow-2xl relative" in:fade>
            <!-- Window Decoration -->
            <div class="h-12 bg-slate-950/80 border-b border-white/5 flex items-center px-8 justify-between">
                <div class="flex items-center gap-2">
                    <div class="w-3 h-3 rounded-full bg-rose-500/50"></div>
                    <div class="w-3 h-3 rounded-full bg-amber-500/50"></div>
                    <div class="w-3 h-3 rounded-full bg-emerald-500/50"></div>
                    <span class="ml-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-500">WealthEngine Core v0.44.1</span>
                </div>
                <div class="flex items-center gap-4">
                    <Terminal class="w-4 h-4 text-indigo-500" />
                    <span class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">log_stream_multiplexer_stdpipe</span>
                </div>
            </div>

            <!-- Terminal Content -->
            <div 
                bind:this={terminalElement}
                class="h-[70vh] overflow-y-auto bg-slate-950/90 p-8 font-mono text-sm selection:bg-indigo-500/30 custom-scrollbar"
            >
                <div class="space-y-1">
                    {#each filteredLogs as line}
                        <div class="group flex gap-4">
                            <span class="text-slate-700 select-none text-[10px] w-12 pt-1 font-bold">{logs.indexOf(line) + 1}</span>
                            <p class="text-slate-300 whitespace-pre-wrap break-all leading-relaxed hover:text-indigo-200 transition-colors">
                                {line}
                            </p>
                        </div>
                    {/each}
                    {#if logs.length === 0}
                        <div class="flex flex-col items-center justify-center h-full opacity-20 py-20">
                            <Terminal class="w-20 h-20 mb-4" />
                            <p class="font-black uppercase tracking-[0.3em] text-xs">Waiting for system frames...</p>
                        </div>
                    {/if}
                </div>
            </div>

            <!-- Status Bar -->
            <div class="h-10 bg-indigo-950/30 border-t border-white/5 flex items-center px-8 justify-between">
                <div class="flex items-center gap-6">
                    <span class="text-[9px] font-black uppercase tracking-widest text-indigo-400">Status: {isPaused ? 'Paused' : 'Streaming'}</span>
                    <span class="text-[9px] font-black uppercase tracking-widest text-slate-500">Encoding: Protobuf v3</span>
                </div>
                <div class="flex items-center gap-4">
                    <div class="w-1.5 h-1.5 rounded-full bg-emerald-500"></div>
                    <span class="text-[9px] font-black uppercase tracking-widest text-emerald-500/80">Socket Secure</span>
                </div>
            </div>
        </div>
    </div>
</div>

<style>
    .glass-card {
        background: rgba(15, 23, 42, 0.8);
        backdrop-filter: blur(24px);
        -webkit-backdrop-filter: blur(24px);
    }

    .gradient-text {
        background: linear-gradient(to right, #6366f1, #a855f7, #ec4899);
        -webkit-background-clip: text;
        -webkit-text-fill-color: transparent;
    }

    .custom-scrollbar::-webkit-scrollbar {
        width: 8px;
    }

    .custom-scrollbar::-webkit-scrollbar-track {
        background: rgba(0, 0, 0, 0.2);
    }

    .custom-scrollbar::-webkit-scrollbar-thumb {
        background: rgba(255, 255, 255, 0.05);
        border-radius: 4px;
    }

    .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: rgba(255, 255, 255, 0.1);
    }
</style>
