<script lang="ts">
    import type { Snippet } from "svelte";

    let {
        variant = "default",
        class: className = "",
        onclick,
        children,
    } = $props<{
        variant?: "default" | "indigo" | "emerald" | "purple" | "rose" | "amber";
        class?: string;
        onclick?: (e: MouseEvent) => void;
        children?: Snippet;
    }>();
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
    role={onclick ? "button" : undefined}
    tabindex={onclick ? 0 : undefined}
    {onclick}
    class="glass-card p-6 md:p-8 space-y-6 transition-all duration-300
    {onclick ? 'cursor-pointer hover:-translate-y-1' : ''}
    {variant === 'indigo' ? 'hover:border-indigo-600' : ''}
    {variant === 'emerald' ? 'hover:border-emerald-600' : ''}
    {variant === 'purple' ? 'hover:border-purple-600' : ''}
    {variant === 'rose' ? 'hover:border-rose-600' : ''}
    {variant === 'amber' ? 'hover:border-amber-600' : ''}
    {className}"
>
    {@render children?.()}
</div>
