<script lang="ts">
    import { Loader2 } from "@lucide/svelte";
    import type { Snippet } from "svelte";

    let {
        type = "button",
        variant = "primary",
        loading = false,
        loadingLabel = "",
        disabled = false,
        onclick,
        class: className = "",
        title = "",
        children,
        ...restProps
    } = $props<{
        type?: "button" | "submit" | "reset";
        variant?: "primary" | "secondary" | "danger" | "ghost";
        loading?: boolean;
        loadingLabel?: string;
        disabled?: boolean;
        onclick?: (e: MouseEvent) => void;
        class?: string;
        title?: string;
        children?: Snippet;
        [key: string]: any;
    }>();
</script>

<button
    {type}
    disabled={disabled || loading}
    {onclick}
    {title}
    class="relative flex items-center justify-center gap-2 font-bold rounded-2xl transition-all duration-200 active:scale-[0.98] outline-none disabled:opacity-50 disabled:pointer-events-none
    {variant === 'primary' ? 'px-6 py-3 bg-indigo-600 text-white shadow-lg shadow-indigo-100 hover:bg-indigo-700 dark:shadow-none' : ''}
    {variant === 'secondary' ? 'px-6 py-3 bg-white text-slate-700 border border-slate-200 hover:bg-slate-50 hover:border-slate-300 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700 dark:hover:bg-slate-700' : ''}
    {variant === 'danger' ? 'px-6 py-3 bg-red-600 text-white shadow-lg shadow-red-100 hover:bg-red-700 dark:shadow-none' : ''}
    {variant === 'ghost' ? 'p-2 text-slate-400 hover:text-slate-900 dark:hover:text-slate-100 rounded-xl hover:bg-slate-50 dark:hover:bg-slate-800 border border-transparent' : ''}
    {className}"
    {...restProps}
>
    {#if loading}
        <Loader2 class="w-5 h-5 animate-spin" />
        {#if loadingLabel}
            <span>{loadingLabel}</span>
        {/if}
    {:else}
        {@render children?.()}
    {/if}
</button>
