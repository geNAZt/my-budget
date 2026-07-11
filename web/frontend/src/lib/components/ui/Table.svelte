<script lang="ts">
    import type { Snippet } from "svelte";

    let {
        class: className = "",
        header,
        body,
    } = $props<{
        class?: string;
        header?: Snippet;
        body?: Snippet;
    }>();
</script>

<div class="glass-card overflow-hidden {className}">
    <div class="overflow-x-auto">
        <table class="w-full border-collapse text-left">
            <thead>
                <tr class="border-b border-slate-100 bg-slate-50/50 dark:border-slate-800 dark:bg-slate-800/30">
                    {@render header?.()}
                </tr>
            </thead>
            <tbody class="divide-y divide-slate-100 dark:divide-slate-800">
                {@render body?.()}
            </tbody>
        </table>
    </div>
</div>
