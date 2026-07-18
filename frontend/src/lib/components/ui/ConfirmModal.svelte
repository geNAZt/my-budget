<script lang="ts">
    import { fade, slide } from "svelte/transition";
    import { Trash2 } from "@lucide/svelte";
    import type { Snippet } from "svelte";

    let {
        open = $bindable(false),
        title = "Confirm Action",
        description = "",
        icon = Trash2,
        class: className = "",
        children,
    } = $props<{
        open: boolean;
        title?: string;
        description?: string;
        icon?: any;
        class?: string;
        children?: Snippet;
    }>();
</script>

{#if open}
    <!-- Backdrop -->
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/60"
        onclick={() => (open = false)}
    >
        <!-- Modal Card -->
        <div
            transition:slide
            class="w-full max-w-md bg-white rounded-[30px] shadow-2xl p-10 relative overflow-hidden dark:bg-slate-900 dark:border dark:border-slate-800 {className}"
            onclick={(e) => e.stopPropagation()}
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="text-center mb-8">
                <div class="inline-flex items-center justify-center p-4 bg-rose-50 text-rose-600 dark:bg-rose-500/10 dark:text-rose-400 rounded-2xl mb-6">
                    {#if icon}
                        {@const Icon = icon}
                        <Icon class="w-8 h-8" />
                    {/if}
                </div>
                <h3 class="text-2xl font-black text-slate-900 dark:text-slate-100 mb-2">
                    {title}
                </h3>
                {#if description}
                    <p class="text-slate-500 dark:text-slate-400 font-medium text-sm leading-relaxed">
                        {description}
                    </p>
                {/if}
            </div>

            <div class="flex flex-col gap-3">
                {@render children?.()}
            </div>
        </div>
    </div>
{/if}
