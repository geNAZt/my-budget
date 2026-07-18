<script lang="ts">
    import { fade, slide } from "svelte/transition";
    import { Plus } from "@lucide/svelte";
    import type { Snippet } from "svelte";
    import { onMount } from "svelte";

    let {
        open = $bindable(false),
        title = "",
        subtitle = "",
        maxWidth = "max-w-lg",
        children,
    } = $props<{
        open: boolean;
        title?: string;
        subtitle?: string;
        maxWidth?: string;
        children?: Snippet;
    }>();

    function handleKeyDown(e: KeyboardEvent) {
        if (e.key === "Escape" && open) {
            open = false;
        }
    }

    $effect(() => {
        if (open) {
            window.addEventListener("keydown", handleKeyDown);
            document.body.style.overflow = "hidden";
        } else {
            window.removeEventListener("keydown", handleKeyDown);
            document.body.style.overflow = "";
        }
    });

    onMount(() => {
        return () => {
            window.removeEventListener("keydown", handleKeyDown);
            document.body.style.overflow = "";
        };
    });
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
            class="w-full {maxWidth} bg-white rounded-[30px] shadow-2xl relative overflow-hidden dark:bg-slate-900 dark:border dark:border-slate-800"
            onclick={(e) => e.stopPropagation()}
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <button
                onclick={() => (open = false)}
                class="absolute top-6 right-6 text-slate-400 hover:text-slate-900 dark:hover:text-slate-100 transition-colors"
                aria-label="Close modal"
            >
                <Plus class="w-6 h-6 rotate-45" />
            </button>

            <div class="p-10 space-y-10 max-h-[85vh] overflow-y-auto">
                {#if title || subtitle}
                    <div>
                        {#if title}
                            <h3 class="text-2xl font-black text-slate-900 dark:text-slate-100 tracking-tight">
                                {title}
                            </h3>
                        {/if}
                        {#if subtitle}
                            <p class="text-slate-500 dark:text-slate-400 font-medium text-sm mt-1">
                                {subtitle}
                            </p>
                        {/if}
                    </div>
                {/if}

                <div>
                    {@render children?.()}
                </div>
            </div>
        </div>
    </div>
{/if}
