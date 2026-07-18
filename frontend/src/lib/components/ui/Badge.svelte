<script lang="ts">
    import type { Snippet } from "svelte";

    let {
        variant = "slate",
        class: className = "",
        children,
    } = $props<{
        variant?: "success" | "warning" | "error" | "info" | "brand" | "slate";
        class?: string;
        children?: Snippet;
    }>();
</script>

<span
    class="inline-flex px-2 py-0.5 rounded-md text-[9px] font-black uppercase tracking-[0.2em] transition-colors
    {variant === 'success' ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-400' : ''}
    {variant === 'warning' ? 'bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-400' : ''}
    {variant === 'error' ? 'bg-rose-50 text-rose-600 dark:bg-rose-500/10 dark:text-rose-400' : ''}
    {variant === 'info' ? 'bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-400' : ''}
    {variant === 'brand' ? 'bg-indigo-50 text-indigo-600 dark:bg-indigo-500/10 dark:text-indigo-400' : ''}
    {variant === 'slate' ? 'bg-slate-50 text-slate-600 dark:bg-slate-500/10 dark:text-slate-400' : ''}
    {className}"
>
    {@render children?.()}
</span>
