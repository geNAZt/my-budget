<script lang="ts">
    import { fade, slide } from "svelte/transition";
    import { X } from "@lucide/svelte";

    let {
        open = $bindable(false),
        title = "",
        subtitle = "",
        maxWidthClass = "max-w-xl",
        zIndexClass = "z-[100]",
        children,
    } = $props<{
        open: boolean;
        title?: string;
        subtitle?: string;
        maxWidthClass?: string;
        zIndexClass?: string;
        children?: import("svelte").Snippet;
    }>();
</script>

{#if open}
    <div
        class="fixed inset-0 {zIndexClass} flex items-center justify-center p-6 bg-slate-900/60"
        transition:fade
    >
        <div
            class="w-full {maxWidthClass} bg-white rounded-[30px] shadow-2xl relative overflow-hidden"
            transition:slide
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="p-8 space-y-6">
                {#if title || subtitle}
                    <div class="flex items-center justify-between">
                        <div class="space-y-1">
                            {#if title}
                                <h2 class="text-xl font-black text-slate-900 tracking-tight">
                                    {title}
                                </h2>
                            {/if}
                            {#if subtitle}
                                <p class="text-slate-500 font-medium text-xs">
                                    {subtitle}
                                </p>
                            {/if}
                        </div>
                        <button
                            onclick={() => (open = false)}
                            class="p-2 hover:bg-slate-100 rounded-xl transition-all border border-transparent hover:border-slate-200"
                        >
                            <X class="w-5 h-5 text-slate-400" />
                        </button>
                    </div>
                {/if}

                {#if children}
                    {@render children()}
                {/if}
            </div>
        </div>
    </div>
{/if}
