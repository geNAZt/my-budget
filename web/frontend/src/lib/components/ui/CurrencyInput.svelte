<script lang="ts">
    import { Euro } from "@lucide/svelte";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";

    let {
        value = $bindable(0),
        label = "Amount (€)",
        placeholder = "0,00",
        required = false,
        disabled = false,
        class: className = "",
        ...restProps
    } = $props<{
        value: number;
        label?: string;
        placeholder?: string;
        required?: boolean;
        disabled?: boolean;
        class?: string;
        [key: string]: any;
    }>();

    let displayValue = $state("");
    let isFocused = $state(false);

    // Sync external value to displayValue when not focused
    $effect(() => {
        if (!isFocused) {
            displayValue = formatGermanAmount(value);
        }
    });

    function handleInput(e: Event) {
        const target = e.target as HTMLInputElement;
        displayValue = target.value;
        value = parseGermanAmount(displayValue);
    }

    function handleBlur() {
        isFocused = false;
        displayValue = formatGermanAmount(value);
    }

    function handleFocus() {
        isFocused = true;
    }
</script>

<div class="space-y-2 w-full">
    {#if label}
        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
            {label}
        </label>
    {/if}
    <div class="relative">
        <div class="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none">
            <Euro class="w-4 h-4 text-slate-400" />
        </div>
        <input
            type="text"
            value={displayValue}
            oninput={handleInput}
            onblur={handleBlur}
            onfocus={handleFocus}
            {placeholder}
            {required}
            {disabled}
            class="block w-full pl-10 pr-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold dark:bg-slate-800 dark:border-slate-700 disabled:opacity-60 disabled:cursor-not-allowed {className}"
            {...restProps}
        />
    </div>
</div>
