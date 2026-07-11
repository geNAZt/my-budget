<script lang="ts">
    import { Layers, Plus, Trash2, X } from "@lucide/svelte";
    import { fade } from "svelte/transition";
    import Modal from "$lib/components/ui/Modal.svelte";
    import Button from "$lib/components/ui/Button.svelte";
    import Input from "$lib/components/ui/Input.svelte";

    let {
        open = $bindable(false),
        editingAssetObj = $bindable(),
        simulatedYields = {},
        assetSaveError = $bindable(null),
        onSave = () => {},
        toInputMonth = (d: string | null) => "",
        fromInputMonth = (val: string) => "",
    } = $props<{
        open: boolean;
        editingAssetObj: any;
        simulatedYields: Record<string, number>;
        assetSaveError: string | null;
        onSave: () => Promise<void>;
        toInputMonth: (d: string | null) => string;
        fromInputMonth: (val: string) => string;
    }>();

    function addEditingSubAsset() {
        if (!editingAssetObj.activeVersion.subAssets) {
            editingAssetObj.activeVersion.subAssets = [];
        }
        editingAssetObj.activeVersion.subAssets.push({
            id: "sa_" + Math.random().toString(36).substring(2, 11),
            name: "",
            targetValue: 0,
            amountPerMonth: 0,
            startDate: new Date().toISOString().substring(0, 7) + "-01T00:00:00Z",
            endDate: null,
            isRemainderConsumer: false,
            dumpingLoanId: "",
            remainderStartDate: "",
            earliestDumpDate: "",
            expenseId: "",
            remainderPriority: 0
        });
        editingAssetObj.activeVersion.subAssets = [...editingAssetObj.activeVersion.subAssets];
    }

    function removeEditingSubAsset(idx: number) {
        editingAssetObj.activeVersion.subAssets.splice(idx, 1);
        editingAssetObj.activeVersion.subAssets = [...editingAssetObj.activeVersion.subAssets];
    }
</script>

<Modal
    bind:open
    title="Edit Asset &amp; Sub-Assets"
    subtitle="Configure parameters and sub-asset pockets."
>
    {#if editingAssetObj}
        <div class="space-y-6">
            {#if assetSaveError}
                <div class="p-4 bg-rose-50 border border-rose-200 text-rose-800 rounded-2xl text-xs font-bold flex gap-2">
                    <X class="w-4 h-4 shrink-0 mt-0.5" />
                    <span>{assetSaveError}</span>
                </div>
            {/if}

            <!-- Parent Asset Form -->
            <div class="space-y-4">
                <h4 class="text-xs font-black uppercase text-indigo-650 tracking-wider ml-1">Asset Configuration</h4>
                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <Input
                        label="Asset Name"
                        bind:value={editingAssetObj.name}
                    />
                    <div class="space-y-1">
                        <label class="text-[10px] font-black uppercase text-slate-400 block ml-1">Expected Return (Simulated) %</label>
                        <input
                            type="number"
                            step="0.01"
                            value={simulatedYields[editingAssetObj.id] !== undefined ? Number(simulatedYields[editingAssetObj.id].toFixed(2)) : (editingAssetObj.activeVersion.interestRate || 0)}
                            disabled
                            class="w-full px-4 py-3 rounded-xl border border-slate-200 bg-slate-100 text-sm font-bold outline-none text-slate-550 cursor-not-allowed dark:bg-slate-800 dark:border-slate-700 dark:text-slate-500 shadow-inner"
                        />
                    </div>
                </div>

                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <Input
                        type="number"
                        label="Monthly Deposit"
                        bind:value={editingAssetObj.activeVersion.amountPerMonth}
                    />
                    <Input
                        label="Target Value"
                        bind:value={editingAssetObj.activeVersion.targetValue}
                    />
                </div>

                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <Input
                        type="month"
                        label="Start Period"
                        value={toInputMonth(editingAssetObj.activeVersion.startDate)}
                        oninput={(e: any) => editingAssetObj.activeVersion.startDate = fromInputMonth(e.target.value)}
                    />
                    <Input
                        type="month"
                        label="End Period"
                        value={toInputMonth(editingAssetObj.activeVersion.endDate)}
                        oninput={(e: any) => editingAssetObj.activeVersion.endDate = fromInputMonth(e.target.value)}
                    />
                </div>
            </div>

            <!-- Sub-Assets/Savings Targets Form List -->
            <div class="space-y-4 pt-6 border-t border-slate-150 dark:border-slate-850">
                <div class="flex items-center justify-between">
                    <h4 class="text-xs font-black uppercase text-cyan-650 tracking-wider ml-1">Sub-Asset Pockets (Funding targets)</h4>
                    <button
                        type="button"
                        onclick={addEditingSubAsset}
                        class="px-3 py-1.5 bg-cyan-50 hover:bg-cyan-100 border border-cyan-100 text-cyan-700 rounded-xl text-[10px] font-black uppercase tracking-widest flex items-center gap-1 transition-all"
                    >
                        <Plus class="w-3.5 h-3.5" /> Add sub-asset
                    </button>
                </div>

                <div class="space-y-4">
                    {#if !editingAssetObj.activeVersion.subAssets || editingAssetObj.activeVersion.subAssets.length === 0}
                        <div class="p-6 border-2 border-dashed border-slate-150 rounded-2xl text-center text-xs text-slate-400 font-medium dark:border-slate-800">
                            No linked sub-asset funding pockets exist. Add one to trace intermediate savings goals.
                        </div>
                    {:else}
                        {#each editingAssetObj.activeVersion.subAssets as sa, idx}
                            <div class="p-4 bg-slate-50 dark:bg-slate-800/40 border dark:border-slate-750 rounded-2xl space-y-4 relative shadow-sm">
                                <button
                                    type="button"
                                    onclick={() => removeEditingSubAsset(idx)}
                                    class="absolute top-2 right-2 text-slate-400 hover:text-red-500 transition-colors p-1 cursor-pointer"
                                    title="Delete sub-asset"
                                >
                                    <Trash2 class="w-4 h-4" />
                                </button>

                                <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                                    <div class="space-y-1">
                                        <span class="text-[9px] font-black uppercase text-slate-400 block ml-1">Pocket Name</span>
                                        <input
                                            type="text"
                                            bind:value={sa.name}
                                            class="w-full px-3 py-2 rounded-xl border border-slate-200 bg-white text-xs font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                        />
                                    </div>
                                    <div class="space-y-1">
                                        <span class="text-[9px] font-black uppercase text-slate-400 block ml-1">Target value (€)</span>
                                        <input
                                            type="number"
                                            bind:value={sa.targetValue}
                                            class="w-full px-3 py-2 rounded-xl border border-slate-200 bg-white text-xs font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                        />
                                    </div>
                                </div>

                                <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
                                    <div class="space-y-1">
                                        <span class="text-[9px] font-black uppercase text-slate-400 block ml-1">Monthly Contribution</span>
                                        <input
                                            type="number"
                                            bind:value={sa.amountPerMonth}
                                            class="w-full px-3 py-2 rounded-xl border border-slate-200 bg-white text-xs font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                        />
                                    </div>
                                    <div class="space-y-1">
                                        <span class="text-[9px] font-black uppercase text-slate-400 block ml-1">Start Date</span>
                                        <input
                                            type="month"
                                            value={toInputMonth(sa.startDate)}
                                            onchange={(e) => sa.startDate = fromInputMonth(e.currentTarget.value)}
                                            class="w-full px-3 py-2 rounded-xl border border-slate-200 bg-white text-xs font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                        />
                                    </div>
                                    <div class="space-y-1">
                                        <span class="text-[9px] font-black uppercase text-slate-400 block ml-1">End Date</span>
                                        <input
                                            type="month"
                                            value={toInputMonth(sa.endDate)}
                                            onchange={(e) => sa.endDate = fromInputMonth(e.currentTarget.value)}
                                            class="w-full px-3 py-2 rounded-xl border border-slate-200 bg-white text-xs font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                        />
                                    </div>
                                </div>
                            </div>
                        {/each}
                    {/if}
                </div>
            </div>
        </div>

        <div class="pt-6 flex justify-end gap-3 border-t border-slate-100 dark:border-slate-800 -mx-6 -mb-6 p-6 mt-6 bg-slate-50 dark:bg-slate-900/50">
            <Button variant="secondary" onclick={() => open = false} class="px-6">
                Cancel
            </Button>
            <Button onclick={onSave} class="px-6">
                Save Changes
            </Button>
        </div>
    {/if}
</Modal>
