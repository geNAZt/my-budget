<script lang="ts">
    let {
        value = $bindable(),
        type = "text",
        placeholder = "",
        required = false,
        label = "",
        class: className = "",
        ...restProps
    } = $props<{
        value: any;
        type?: string;
        placeholder?: string;
        required?: boolean;
        label?: string;
        class?: string;
        [key: string]: any;
    }>();
</script>

<div class="space-y-2 w-full">
    {#if label}
        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
            {label}
        </label>
    {/if}
    <input
        {type}
        {placeholder}
        {required}
        bind:value={value}
        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold dark:bg-slate-800 dark:border-slate-700 {className}"
        {...restProps}
    />
</div>
