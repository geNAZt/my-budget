<script lang="ts">
    import { onMount } from "svelte";
    import * as api from "$lib/gen/api_pb.js";
    import { wsCall, onWsEvent } from "$lib/utils/ws_fetch";
    import { Activity } from "@lucide/svelte";

    let backendVersion = $state("...");
    let watchtowerStatus = $state("green");
    const frontendVersion = import.meta.env.VITE_GIT_COMMIT || "dev";

    onMount(() => {
        let unsubscribe: (() => void) | undefined;

        (async () => {
            // Get initial status
            const [resp] = await wsCall(
                "system::status",
                api.EmptySchema,
                {},
                [api.SystemStatusSchema]
            ).one();

            if (resp) {
                backendVersion = resp.backendVersion || "dev";
                watchtowerStatus = resp.watchtowerStatus || "green";
            }

            // Subscribe to updates
            unsubscribe = onWsEvent("system::status", api.SystemStatusSchema, (data) => {
                backendVersion = data.backendVersion || "dev";
                watchtowerStatus = data.watchtowerStatus || "green";
            });
        })();

        return () => {
            if (unsubscribe) unsubscribe();
        };
    });

    function getStatusColor(status: string) {
        switch (status) {
            case "green": return "bg-emerald-500";
            case "yellow": return "bg-amber-500";
            case "yellow-blinking": return "bg-amber-500 animate-pulse";
            case "red": return "bg-rose-500";
            default: return "bg-slate-500";
        }
    }
</script>

<div class="fixed bottom-4 right-4 z-50">
    <div class="glass-card !p-3 flex items-center gap-4 shadow-2xl border-white/40">
        <div class="flex flex-col">
            <div class="flex items-center gap-2">
                <Activity class="h-3 w-3 text-slate-400" />
                <span class="text-[9px] font-black uppercase tracking-widest text-slate-400">Version Context</span>
            </div>
            <div class="flex items-center gap-3 mt-1">
                <div class="flex flex-col">
                    <span class="text-[8px] font-black uppercase text-slate-400 leading-none mb-1">Frontend</span>
                    <span class="text-[10px] font-bold text-slate-700 font-mono">
                        {frontendVersion.substring(0, 7)}
                    </span>
                </div>
                <div class="w-px h-6 bg-slate-100"></div>
                <div class="flex flex-col">
                    <span class="text-[8px] font-black uppercase text-slate-400 leading-none mb-1">Backend</span>
                    <span class="text-[10px] font-bold text-slate-700 font-mono">
                        {backendVersion.substring(0, 7)}
                    </span>
                </div>
            </div>
        </div>

        <div class="flex flex-col items-center pl-2 border-l border-slate-100">
            <span class="text-[8px] font-black uppercase text-slate-400 leading-none mb-1.5">System</span>
            <div class="relative">
                <div class="h-3 w-3 rounded-full {getStatusColor(watchtowerStatus)} shadow-lg shadow-current/20"></div>
                {#if watchtowerStatus === 'yellow-blinking'}
                    <div class="absolute inset-0 h-3 w-3 rounded-full bg-amber-500 animate-ping opacity-75"></div>
                {/if}
            </div>
        </div>
    </div>
</div>
