<script lang="ts">
    import { Search, ChevronDown, Check, X } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";

    interface Option {
        id: string;
        label: string;
    }

    let {
        options = [],
        value = $bindable(),
        placeholder = "Select option...",
        label = "",
        onchange = null,
    } = $props<{
        options: Option[];
        value: string | null | undefined;
        placeholder?: string;
        label?: string;
        onchange?: ((val: string) => void) | null;
    }>();

    let isOpen = $state(false);
    let searchQuery = $state("");
    let dropdownElement = $state<HTMLDivElement>();
    let searchInput = $state<HTMLInputElement>();

    $effect(() => {
        if (isOpen && searchInput) {
            searchInput.focus();
        }
    });

    const filteredOptions = $derived(
        (options || []).filter((opt: Option) =>
            (opt?.label || "")
                .toLowerCase()
                .includes((searchQuery || "").toLowerCase()),
        ),
    );

    const selectedLabel = $derived(
        (options || []).find((opt: Option) => opt.id === value)?.label || "",
    );
    function selectOption(id: string) {
        value = id;
        isOpen = false;
        searchQuery = "";
        if (onchange) onchange(id);
    }

    // Close on click outside
    function handleClickOutside(event: MouseEvent) {
        if (
            dropdownElement &&
            !dropdownElement.contains(event.target as Node)
        ) {
            isOpen = false;
            searchQuery = "";
        }
    }
</script>

<svelte:window onclick={handleClickOutside} />

<div
    class="space-y-2 w-full min-w-[200px] flex-shrink-0"
    bind:this={dropdownElement}
>
    {#if label}
        <label
            class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em] ml-1"
            >{label}</label
        >
    {/if}

    <div class="relative">
        <button
            type="button"
            onclick={() => (isOpen = !isOpen)}
            class="w-full flex items-center justify-between px-4 py-3 bg-white border {isOpen
                ? 'border-indigo-500 ring-4 ring-indigo-500/10'
                : 'border-slate-200'} rounded-xl transition-all text-left"
        >
            <span
                class="text-xs font-black uppercase tracking-tight {value
                    ? 'text-slate-900'
                    : 'text-slate-400'}"
            >
                {selectedLabel || placeholder}
            </span>
            <ChevronDown
                class="w-4 h-4 text-slate-400 transition-transform {isOpen
                    ? 'rotate-180'
                    : ''}"
            />
        </button>

        {#if isOpen}
            <div
                transition:fade={{ duration: 100 }}
                class="absolute z-[110] w-full mt-2 bg-white border border-slate-100 rounded-2xl shadow-2xl overflow-hidden"
            >
                <div class="p-3 border-b border-slate-50 bg-slate-50/50">
                    <div class="relative">
                        <Search
                            class="w-3.5 h-3.5 text-slate-400 absolute left-3 top-1/2 -translate-y-1/2"
                        />
                        <input
                            type="text"
                            bind:this={searchInput}
                            bind:value={searchQuery}
                            placeholder="Filter options..."
                            class="w-full pl-9 pr-4 py-2 bg-white border border-slate-200 rounded-lg text-[10px] font-bold outline-none focus:border-indigo-500 focus:ring-4 focus:ring-indigo-500/5 transition-all"
                        />
                    </div>
                </div>

                <div class="max-h-60 overflow-y-auto custom-scrollbar">
                    {#if filteredOptions.length === 0}
                        <div class="p-4 text-center">
                            <p
                                class="text-[10px] font-black text-slate-400 uppercase"
                            >
                                No results found
                            </p>
                        </div>
                    {:else}
                        {#each filteredOptions as opt}
                            <button
                                type="button"
                                onclick={() => selectOption(opt.id)}
                                class="w-full flex items-center justify-between px-4 py-3 hover:bg-indigo-50 transition-colors group border-b border-slate-50 last:border-none"
                            >
                                <span
                                    class="text-xs font-black uppercase tracking-tight {value ===
                                    opt.id
                                        ? 'text-indigo-600'
                                        : 'text-slate-700 group-hover:text-indigo-600'}"
                                >
                                    {opt.label}
                                </span>
                                {#if value === opt.id}
                                    <Check
                                        class="w-3.5 h-3.5 text-indigo-600"
                                    />
                                {/if}
                            </button>
                        {/each}
                    {/if}
                </div>
            </div>
        {/if}
    </div>
</div>

<style>
    .custom-scrollbar::-webkit-scrollbar {
        width: 4px;
    }
    .custom-scrollbar::-webkit-scrollbar-track {
        background: transparent;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #e2e8f0;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: #cbd5e1;
    }
</style>
