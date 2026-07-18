<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import { ExecutionPlanListSchema, ExecutionConnectionListSchema, ExecutionPlanSchema, GenericIDSchema, ExecutionConnectionSchema, ExecutionLogListSchema, ErrorSchema, IntegrationListSchema } from "$lib/gen/api_pb.js";
    import { onMount } from "svelte";
    import { fade, slide } from "svelte/transition";
    import {
        Cpu,
        Play,
        History,
        Plus,
        Save,
        Trash2,
        Settings2,
        AlertCircle,
        CheckCircle2,
        Clock,
        ChevronRight,
        Loader2,
        RefreshCw,
        Database,
        Activity,
        Terminal,
        Key,
        Code2,
        Shield,
        FileCode2,
        Boxes,
    } from "@lucide/svelte";
    import MonacoEditor from "$lib/components/MonacoEditor.svelte";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    let plans = $state<any[]>([]);
    let isLoading = $state(true);
    let isSaving = $state(false);
    let isRunning = $state(false);
    let selectedPlanId = $state<string | null>(null);
    let editedPlan = $state<any>(null);

    let logs = $state<any[]>([]);
    let isLoadingLogs = $state(false);
    let selectedLogId = $state<string | null>(null);
    let selectedLog = $derived(logs.find((l) => l.id === selectedLogId));

    let connections = $state<any[]>([]);
    let integrations = $state<any[]>([]);
    let isAddingSecret = $state(false);
    let newSecretName = $state("");
    let newSecretValue = $state("");
    let secretError = $state("");

    let isFullscreen = $state(false);

    const triggerOptions = [
        { id: "CRON", label: "Periodic Schedule (CRON)" },
        { id: "SYNC_FINISHED", label: "External Bank Sync Complete" },
        { id: "TRANSACTION_NEW", label: "Specific Bank Transaction" },
    ];

    async function loadAllData() {
        isLoading = true;
        try {
            const [plansData, connectionsData, integrationsData] = await Promise.all([
                wsCall("automations::plans::list", null, null, [ExecutionPlanListSchema]).one(),
                wsCall("automations::connections::list", null, null, [ExecutionConnectionListSchema]).one(),
                wsCall("integrations::list", null, null, [IntegrationListSchema]).one(),
            ]);
            plans = plansData[0] ? plansData[0].plans : [];
            connections = connectionsData[0] ? connectionsData[0].connections : [];
            integrations = integrationsData[0] ? integrationsData[0].integrations : [];
            if (plans.length > 0) {
                selectPlan(plans[0]);
            } else {
                startNewPlan();
            }
        } catch (e) {
            console.error("Failed to load automation data:", e);
        } finally {
            isLoading = false;
        }
    }

    function selectPlan(plan: any) {
        selectedPlanId = plan.id;
        editedPlan = {
            id: plan.id,
            name: plan.name,
            code: plan.code,
            triggerType: plan.triggerType,
            triggerValue: plan.triggerValue,
            isEnabled: plan.isEnabled,
        };
        selectedLogId = null;
        loadLogs(plan.id);
    }

    async function loadLogs(planId: string) {
        try {
            isLoadingLogs = true;
            const [resp, err] = await wsCall("automations::plans::logs", GenericIDSchema, {
                id: planId,
            }, [ExecutionLogListSchema]).one();
            if (err) throw err;
            logs = resp?.logs ?? [];
            if (logs.length > 0) {
                selectedLogId = logs[0].id;
            }
        } catch (e) {
            console.error("Failed to load logs:", e);
            logs = [];
        } finally {
            isLoadingLogs = false;
        }
    }

    function getLogDuration(log: any): number {
        if (!log.startedAt || !log.finishedAt) return 0;
        return Math.max(
            0,
            new Date(log.finishedAt).getTime() -
                new Date(log.startedAt).getTime(),
        );
    }

    function startNewPlan() {
        selectedPlanId = "new";
        editedPlan = {
            id: crypto.randomUUID(),
            name: "New ETF Option Buy Plan",
            code: `/** depend on wealthengine:1.0.0 **/
const { WealthEngine } = require('wealthengine');

const wealthengine = new WealthEngine();
const budget = wealthengine.currentBudgetSheet();

console.log("Current budget sheet loaded:", budget);
`,
            triggerType: "CRON",
            triggerValue: "*/5 * * * *",
            isEnabled: true,
        };
        logs = [];
        selectedLogId = null;
    }

    async function savePlan() {
        if (!editedPlan.name.trim()) return;
        try {
            isSaving = true;
            const [saved, err] = await wsCall("automations::plans::save", ExecutionPlanSchema, {
                id: editedPlan.id || "",
                name: editedPlan.name,
                code: editedPlan.code,
                triggerType: editedPlan.triggerType,
                triggerValue: editedPlan.triggerValue,
                isEnabled: editedPlan.isEnabled,
            }, [ExecutionPlanSchema]).one();
            if (err) throw err;

            const index = plans.findIndex((p) => p.id === saved.id);
            if (index !== -1) {
                plans[index] = saved;
            } else {
                plans.push(saved);
            }
            selectedPlanId = saved.id;
            await loadLogs(saved.id);
        } catch (e) {
            console.error("Failed to save plan:", e);
        } finally {
            isSaving = false;
        }
    }

    async function toggleEditedPlanEnabled() {
        editedPlan.isEnabled = !editedPlan.isEnabled;
        if (selectedPlanId && selectedPlanId !== "new") {
            await savePlan();
        }
    }

    async function togglePlanEnabledDirect(plan: any) {
        try {
            const updatedPlan = {
                ...plan,
                isEnabled: !plan.isEnabled,
            };
            const [saved, err] = await wsCall("automations::plans::save", ExecutionPlanSchema, {
                id: updatedPlan.id || "",
                name: updatedPlan.name,
                code: updatedPlan.code,
                triggerType: updatedPlan.triggerType,
                triggerValue: updatedPlan.triggerValue,
                isEnabled: updatedPlan.isEnabled,
            }, [ExecutionPlanSchema]).one();
            if (err) throw err;
            const index = plans.findIndex((p) => p.id === saved.id);
            if (index !== -1) {
                plans[index] = saved;
            }
            if (selectedPlanId === saved.id) {
                editedPlan.isEnabled = saved.isEnabled;
            }
        } catch (e) {
            console.error("Failed to toggle plan direct:", e);
        }
    }

    async function deletePlan(planId: string) {
        if (!confirm("Are you sure you want to delete this automation plan?"))
            return;
        try {
            const [, err] = await wsCall("automations::plans::delete", GenericIDSchema, { id: planId }, [ErrorSchema]).one();
            if (err) throw err;
            plans = plans.filter((p) => p.id !== planId);
            if (selectedPlanId === planId) {
                selectedPlanId = plans.length > 0 ? plans[0].id : null;
                if (selectedPlanId && selectedPlanId !== "new") {
                    selectPlan(plans.find((p) => p.id === selectedPlanId));
                } else {
                    startNewPlan();
                }
            }
        } catch (e) {
            console.error("Failed to delete plan:", e);
        }
    }

    async function runPlanNow() {
        if (selectedPlanId === "new") return;
        try {
            isRunning = true;
            const [, err] = await wsCall("automations::plans::run", GenericIDSchema, { id: selectedPlanId }, [ErrorSchema]).one();
            if (err) throw err;
            // Reload logs after slight delay to allow runner execution and logging
            setTimeout(() => loadLogs(selectedPlanId!), 1000);
        } catch (e: any) {
            alert(e.message);
        } finally {
            isRunning = false;
        }
    }

    async function createConnection() {
        secretError = "";
        const name = newSecretName.trim();
        const val = newSecretValue.trim();
        if (!name || !val) {
            secretError = "Name and secret value are required";
            return;
        }

        try {
            isAddingSecret = true;
            const [conn, err] = await wsCall("automations::connections::save", ExecutionConnectionSchema, {
                id: "",
                name: name,
                value: val,
            }, [ExecutionConnectionSchema]).one();
            if (err) throw err;
            connections.push(conn);
            newSecretName = "";
            newSecretValue = "";
            secretError = "";
        } catch (e: any) {
            secretError = `Connection failed: ${e.message}`;
        } finally {
            isAddingSecret = false;
        }
    }

    async function deleteConnection(id: string) {
        if (!confirm("Permanently delete this secure connection?")) return;
        try {
            const [, err] = await wsCall("automations::connections::delete", GenericIDSchema, { id: id }, [ErrorSchema]).one();
            if (err) throw err;
            connections = connections.filter((c) => c.id !== id);
        } catch (e) {
            console.error("Failed to delete connection:", e);
        }
    }

    onMount(() => {
        loadAllData();
    });

    function describeCron(expr: string): string {
        return "Runs roughly every 5 minutes (internal scheduler)";
    }

    const syncTriggerOptions = $derived([
        { id: "ALL", label: "Any Bank Integration" },
        ...integrations.map((i) => ({ id: i.id, label: i.name })),
    ]);

    const bankTriggerOptions = [
        { id: "ANY", label: "Any Incoming Transaction" },
    ];
</script>

<svelte:head>
    <title>Automations &amp; Workflows — BudgetScript</title>
</svelte:head>

<div class="space-y-12">
    <!-- Header Hero Section -->
    <header
        class="flex flex-col md:flex-row md:items-end justify-between gap-6"
    >
        <div class="space-y-2">
            <h1 class="text-5xl font-black tracking-tight text-slate-900">
                Automation <span class="gradient-text">Lab</span>.
            </h1>
            <p class="text-slate-500 font-medium text-lg">
                Event-driven execution & sandboxing.
            </p>
        </div>

        <button
            onclick={startNewPlan}
            class="btn-primary flex items-center gap-2"
        >
            <Plus class="w-4 h-4" />
            Forge New Plan
        </button>
    </header>

    <!-- Loading State Shield -->
    {#if isLoading}
        <div
            class="glass-card flex flex-col items-center justify-center p-20 space-y-4"
        >
            <Loader2 class="w-10 h-10 text-indigo-600 animate-spin" />
            <p
                class="text-sm font-black text-slate-400 uppercase tracking-widest"
            >
                Scanning Execution Registry...
            </p>
        </div>
    {:else}
        <div class="grid grid-cols-1 lg:grid-cols-12 gap-8 items-start">
            <!-- Sidebar: Plan Explorer -->
            <div class="lg:col-span-4 space-y-6 lg:sticky lg:top-8">
                <!-- Plans List -->
                <div class="glass-card p-6 space-y-4">
                    <div class="flex items-center justify-between">
                        <span
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                            >Plan Registry</span
                        >
                        <span
                            class="px-2 py-0.5 bg-indigo-50 text-indigo-600 rounded text-[10px] font-black uppercase tracking-wider"
                            >{plans.length} Nodes</span
                        >
                    </div>

                    <div class="space-y-3 max-h-[350px] overflow-y-auto pr-1">
                        {#if plans.length === 0}
                            <div
                                class="text-center py-8 bg-slate-50 border border-dashed border-slate-200 rounded-2xl p-4"
                            >
                                <Activity
                                    class="w-8 h-8 text-slate-200 mx-auto mb-2"
                                />
                                <p
                                    class="text-[10px] font-black text-slate-400 uppercase tracking-widest"
                                >
                                    Registry Empty
                                </p>
                            </div>
                        {:else}
                            {#each plans as plan}
                                <div
                                    class="w-full text-left p-4 rounded-2xl border transition-all flex items-center justify-between group
                                        {selectedPlanId === plan.id
                                        ? 'bg-indigo-600 border-indigo-600 text-white shadow-xl shadow-indigo-100'
                                        : 'bg-white border-slate-100 hover:border-indigo-200 hover:shadow-md'}"
                                >
                                    <button
                                        onclick={() => selectPlan(plan)}
                                        class="flex-1 text-left space-y-1 overflow-hidden bg-transparent border-none p-0"
                                    >
                                        <h4
                                            class="font-black text-sm tracking-tight truncate pr-4 {selectedPlanId ===
                                            plan.id
                                                ? 'text-white'
                                                : 'text-slate-900'}"
                                        >
                                            {plan.name}
                                        </h4>
                                        <div
                                            class="flex items-center gap-1.5 text-[9px] font-black uppercase tracking-[0.1em]
                                            {selectedPlanId === plan.id
                                                ? 'text-indigo-200'
                                                : 'text-slate-400'}"
                                        >
                                            {#if plan.triggerType === "CRON"}
                                                <Clock class="w-2.5 h-2.5" />
                                                Cron: {plan.triggerValue}
                                            {:else}
                                                <Activity class="w-2.5 h-2.5" />
                                                Event: {plan.triggerType}
                                            {/if}
                                        </div>
                                    </button>

                                    <div class="flex items-center gap-3 ml-2">
                                        <button
                                            onclick={(e) => {
                                                e.stopPropagation();
                                                togglePlanEnabledDirect(plan);
                                            }}
                                            class="h-5 w-5 rounded-full flex items-center justify-center transition-all
                                                {plan.isEnabled
                                                ? 'bg-emerald-400 animate-pulse'
                                                : selectedPlanId === plan.id
                                                  ? 'bg-indigo-300'
                                                  : 'bg-slate-300'}"
                                        ></button>

                                        <ChevronRight
                                            class="w-4 h-4 transition-transform group-hover:translate-x-0.5
                                            {selectedPlanId === plan.id
                                                ? 'text-indigo-200'
                                                : 'text-slate-300'}"
                                        />
                                    </div>
                                </div>
                            {/each}
                        {/if}
                    </div>
                </div>

                <!-- Secrets / Connections Vault -->
                <div class="glass-card p-6 space-y-6">
                    <div class="space-y-4">
                        <div class="flex items-center gap-2">
                            <Shield class="w-4 h-4 text-indigo-600" />
                            <span
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-600"
                                >Secrets Vault</span
                            >
                        </div>

                        <div class="space-y-3">
                            <input
                                type="text"
                                placeholder="e.g. BOT_TOKEN"
                                bind:value={newSecretName}
                                class="w-full bg-white border border-slate-200 rounded-xl px-3 py-2 text-xs font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                            />
                            <input
                                type="password"
                                placeholder="Secure credential string"
                                bind:value={newSecretValue}
                                class="w-full bg-white border border-slate-200 rounded-xl px-3 py-2 text-xs font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                            />

                            {#if secretError}
                                <p
                                    class="text-[10px] text-rose-500 font-bold flex items-center gap-1"
                                >
                                    <AlertCircle
                                        class="w-3.5 h-3.5 flex-shrink-0"
                                    />
                                    {secretError}
                                </p>
                            {/if}

                            <button
                                onclick={createConnection}
                                disabled={isAddingSecret}
                                class="w-full btn-primary !py-2.5 !text-[10px] font-black uppercase tracking-wider flex justify-center items-center gap-2"
                            >
                                {#if isAddingSecret}
                                    <RefreshCw
                                        class="w-3.5 h-3.5 animate-spin"
                                    />
                                {:else}
                                    <Key class="w-3.5 h-3.5" />
                                {/if}
                                Provision Slot
                            </button>
                        </div>
                    </div>

                    <div class="space-y-3 pt-4 border-t border-slate-100">
                        <span
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                            >Active Secure Slots</span
                        >
                        {#if connections.length === 0}
                            <p
                                class="text-[10px] text-slate-400 italic text-center py-2 bg-slate-50 rounded-xl"
                            >
                                No connections configured. Secrets vault empty.
                            </p>
                        {:else}
                            <div
                                class="space-y-2 max-h-[200px] overflow-y-auto pr-1"
                            >
                                {#each connections as conn}
                                    <div
                                        class="flex items-center justify-between p-3 bg-white border border-slate-100 rounded-xl shadow-sm"
                                    >
                                        <div
                                            class="flex items-center gap-2 overflow-hidden"
                                        >
                                            <div
                                                class="p-1.5 bg-slate-50 rounded-lg shrink-0"
                                            >
                                                <Terminal
                                                    class="w-3 h-3 text-slate-400"
                                                />
                                            </div>
                                            <span
                                                class="text-xs font-bold text-slate-700 truncate"
                                                >{conn.name}</span
                                            >
                                        </div>
                                        <button
                                            onclick={() =>
                                                deleteConnection(conn.id)}
                                            class="p-1.5 text-slate-300 hover:text-rose-600 transition-colors"
                                        >
                                            <Trash2 class="w-3.5 h-3.5" />
                                        </button>
                                    </div>
                                {/each}
                            </div>
                        {/if}
                    </div>
                </div>
            </div>

            <!-- Main Content: Plan Editor & Logs -->
            <div class="lg:col-span-8 space-y-8">
                {#if !selectedPlanId}
                    <!-- Welcome Centered View -->
                    <div
                        class="glass-card p-12 flex flex-col items-center justify-center text-center space-y-4"
                    >
                        <div class="p-6 bg-indigo-50 rounded-3xl">
                            <Boxes class="w-12 h-12 text-indigo-600" />
                        </div>
                        <div class="max-w-md">
                            <h3
                                class="text-xl font-black text-slate-900 mb-2 tracking-tight"
                            >
                                Select an Automation Script
                            </h3>
                            <p
                                class="text-xs text-slate-400 font-medium leading-relaxed"
                            >
                                Choose an existing automation plan from the
                                registry or create a new one to begin
                                orchestrating your financial data via secure
                                sandboxed JavaScript.
                            </p>
                        </div>
                        <button onclick={startNewPlan} class="btn-primary px-8">
                            Forge New Plan
                        </button>
                    </div>
                {:else if editedPlan}
                    <!-- Plan Editor Surface -->
                    <div
                        class="glass-card p-8 space-y-8 relative overflow-hidden"
                        transition:fade
                    >
                        <div
                            class="flex flex-col md:flex-row md:items-center justify-between gap-6"
                        >
                            <div class="flex-1 space-y-4">
                                <div class="space-y-1">
                                    <label
                                        class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em] ml-1"
                                        >Plan Identifier</label
                                    >
                                    <input
                                        type="text"
                                        bind:value={editedPlan.name}
                                        placeholder="Enter plan name"
                                        class="text-xl font-black text-slate-900 bg-transparent border-none outline-none focus:ring-0 p-0 w-full tracking-tight"
                                    />
                                </div>

                                <div class="flex items-center gap-3">
                                    <button
                                        onclick={toggleEditedPlanEnabled}
                                        class="flex items-center gap-2 px-3 py-1.5 rounded-full border transition-all text-[10px] font-black uppercase tracking-wider
                                            {editedPlan.isEnabled
                                            ? 'bg-emerald-50 border-emerald-200 text-emerald-700'
                                            : 'bg-slate-50 border-slate-200 text-slate-400'}"
                                    >
                                        <div
                                            class="h-2 w-2 rounded-full {editedPlan.isEnabled
                                                ? 'bg-emerald-500 animate-pulse'
                                                : 'bg-slate-400'}"
                                        ></div>
                                        <span
                                            >{editedPlan.isEnabled
                                                ? "Active/Enabled"
                                                : "Disabled"}</span
                                        >
                                    </button>

                                    {#if selectedPlanId !== "new"}
                                        <button
                                            onclick={() =>
                                                deletePlan(selectedPlanId!)}
                                            class="p-2.5 text-slate-400 hover:text-rose-600 hover:bg-rose-50 rounded-xl transition-all border border-transparent hover:border-rose-100"
                                            title="Archive Plan"
                                        >
                                            <Trash2 class="w-4 h-4" />
                                        </button>
                                    {/if}
                                </div>
                            </div>
                        </div>

                        <!-- Configuration Hub -->
                        <div class="grid grid-cols-1 md:grid-cols-2 gap-8 pt-4">
                            <div class="space-y-4">
                                <SearchableDropdown
                                    label="Execution Trigger Event"
                                    options={triggerOptions}
                                    bind:value={editedPlan.triggerType}
                                    placeholder="Choose trigger event..."
                                    onchange={() => {
                                        if (
                                            editedPlan.triggerType === "CRON"
                                        ) {
                                            editedPlan.triggerValue =
                                                "*/5 * * * *";
                                        } else {
                                            editedPlan.triggerValue = "";
                                        }
                                    }}
                                />
                            </div>

                            <div class="space-y-1">
                                {#if editedPlan.triggerType === "CRON"}
                                    <label
                                        class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em] ml-1 mb-1"
                                    >
                                        Execution Interval (UNIX Cron)
                                    </label>
                                    <div class="relative">
                                        <input
                                            type="text"
                                            bind:value={
                                                editedPlan.triggerValue
                                            }
                                            placeholder="*/5 * * * *"
                                            class="w-full bg-white border border-slate-200 rounded-2xl px-4 py-3 text-sm font-mono focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                        />
                                    </div>
                                    <p
                                        class="text-[9px] text-slate-400 font-bold uppercase tracking-wider flex items-center gap-1.5 ml-1 mt-1.5"
                                    >
                                        <Clock class="w-3.5 h-3.5" />
                                        {describeCron(editedPlan.triggerValue)}
                                    </p>
                                {:else}
                                    <SearchableDropdown
                                        label={editedPlan.triggerType ===
                                        "SYNC_FINISHED"
                                            ? "Data Chain Selection"
                                            : "Bank Transaction Filter"}
                                        options={editedPlan.triggerType ===
                                        "SYNC_FINISHED"
                                            ? syncTriggerOptions
                                            : bankTriggerOptions}
                                        bind:value={editedPlan.triggerValue}
                                        placeholder={editedPlan.triggerType ===
                                        "SYNC_FINISHED"
                                            ? "Pick integration trigger..."
                                            : "Pick account trigger..."}
                                    />
                                    <p
                                        class="text-[10px] text-slate-400 font-medium ml-1 mt-1.5 leading-relaxed"
                                    >
                                        {#if editedPlan.triggerType === "SYNC_FINISHED"}
                                            Script receives data chain info
                                            inside global <code
                                                class="bg-slate-100 p-0.5 rounded text-indigo-600 font-mono"
                                                >trigger.data</code
                                            >
                                            (e.g.
                                            <code class="text-indigo-600"
                                                >integration_name</code
                                            >,
                                            <code class="text-indigo-600"
                                                >service_type</code
                                            >,
                                            <code class="text-indigo-600"
                                                >discovered_transactions</code
                                            >).
                                        {:else}
                                            Script fires for every incoming
                                            transaction matching the selected
                                            filter. Access via <code
                                                class="bg-slate-100 p-0.5 rounded text-indigo-600 font-mono"
                                                >trigger.data</code
                                            >.
                                        {/if}
                                    </p>
                                {/if}
                            </div>
                        </div>

                        <!-- IDE Canvas -->
                        <div class="space-y-4 pt-4 border-t border-slate-100">
                            <div class="flex items-center justify-between">
                                <div class="flex items-center gap-2">
                                    <Code2 class="w-4 h-4 text-indigo-600" />
                                    <span
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-600"
                                        >Secure Sandboxed JavaScript</span
                                    >
                                </div>
                                <button
                                    type="button"
                                    onclick={() => (isFullscreen = true)}
                                    class="text-[9px] font-black uppercase text-indigo-600 hover:text-indigo-800 bg-indigo-50 hover:bg-indigo-100 px-2.5 py-1 rounded-lg transition-all"
                                >
                                    Focus IDE
                                </button>
                            </div>

                            <div
                                class="bg-slate-50 p-4 rounded-2xl border border-slate-200"
                            >
                                <p
                                    class="text-[10px] text-slate-500 font-medium leading-relaxed mb-3"
                                >
                                    Full access to <code class="text-indigo-600"
                                        >WealthEngine</code
                                    >
                                    for budget sheet manipulation. Network and filesystem
                                    access restricted. Use
                                    <code class="text-indigo-600"
                                        >secrets.get('NAME')</code
                                    > for Vault access.
                                </p>
                                <div class="h-[480px]">
                                    <MonacoEditor
                                        bind:value={editedPlan.code}
                                    />
                                </div>
                            </div>

                            <div class="flex items-center justify-between pt-4">
                                <div class="flex items-center gap-4">
                                    {#if selectedPlanId !== "new"}
                                        <button
                                            onclick={runPlanNow}
                                            disabled={isRunning || isSaving}
                                            class="flex items-center gap-2 text-xs font-black uppercase tracking-wider px-5 py-3 rounded-xl border border-indigo-200 bg-indigo-50 hover:bg-indigo-100 text-indigo-600 transition-all disabled:opacity-50"
                                        >
                                            {#if isRunning}
                                                <RefreshCw
                                                    class="w-4 h-4 animate-spin"
                                                />
                                            {:else}
                                                <Play class="w-4 h-4" />
                                            {/if}
                                            Run Now
                                        </button>
                                    {/if}
                                </div>

                                <button
                                    onclick={savePlan}
                                    disabled={isSaving ||
                                        !editedPlan.name.trim()}
                                    class="btn-primary flex items-center gap-2 disabled:opacity-50"
                                >
                                    {#if isSaving}
                                        <RefreshCw
                                            class="w-4 h-4 animate-spin"
                                        />
                                    {:else}
                                        <Save class="w-4 h-4" />
                                    {/if}
                                    Commit Plan to Registry
                                </button>
                            </div>
                        </div>
                    </div>

                    <!-- Logs Audit Section -->
                    {#if selectedPlanId && selectedPlanId !== "new"}
                        <div class="glass-card p-8 space-y-6">
                            <div class="flex items-center justify-between">
                                <div class="flex items-center gap-2">
                                    <Terminal class="w-4 h-4 text-slate-400" />
                                    <span
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400"
                                        >Execution Audit Logs</span
                                    >
                                </div>
                                <button
                                    onclick={() => loadLogs(selectedPlanId!)}
                                    disabled={isLoadingLogs}
                                    class="p-1.5 text-slate-400 hover:text-indigo-600 hover:bg-slate-50 rounded-lg transition-all"
                                    title="Reload Logs"
                                >
                                    <RefreshCw
                                        class="w-4 h-4 {isLoadingLogs
                                            ? 'animate-spin'
                                            : ''}"
                                    />
                                </button>
                            </div>

                            {#if isLoadingLogs}
                                <div
                                    class="flex items-center justify-center py-12"
                                >
                                    <Loader2
                                        class="w-6 h-6 text-slate-200 animate-spin"
                                    />
                                </div>
                            {:else if logs.length === 0}
                                <p
                                    class="text-xs text-slate-400 italic text-center py-8 bg-slate-50 rounded-2xl"
                                >
                                    No logs found. Run the plan to inspect
                                    execution metrics.
                                </p>
                            {:else}
                                <div
                                    class="grid grid-cols-1 md:grid-cols-12 gap-6"
                                >
                                    <!-- History Timeline -->
                                    <div
                                        class="md:col-span-4 space-y-2 max-h-[300px] overflow-y-auto pr-1"
                                    >
                                        {#each logs as log}
                                            <button
                                                onclick={() =>
                                                    (selectedLogId = log.id)}
                                                class="w-full text-left p-3 rounded-xl border text-[11px] font-mono tracking-tight flex items-center justify-between transition-all
                                                    {selectedLogId === log.id
                                                    ? 'bg-slate-900 border-slate-900 text-white font-bold'
                                                    : 'bg-white border-slate-100 hover:border-slate-200 text-slate-500'}"
                                            >
                                                <div
                                                    class="flex items-center gap-2 truncate"
                                                >
                                                    <div
                                                        class="h-1.5 w-1.5 rounded-full {log.status ===
                                                        'SUCCESS'
                                                            ? 'bg-emerald-400'
                                                            : 'bg-rose-400'}"
                                                    ></div>
                                                    <span class="truncate"
                                                        >{new Date(
                                                            log.startedAt,
                                                        ).toLocaleString(
                                                            "de-DE",
                                                        )}</span
                                                    >
                                                </div>
                                                <div
                                                    class="text-[9px] uppercase tracking-wider
                                                    {selectedLogId === log.id
                                                        ? 'text-slate-400'
                                                        : 'text-slate-400'}"
                                                >
                                                    Duration: {getLogDuration(
                                                        log,
                                                    )}ms
                                                </div>
                                            </button>
                                        {/each}
                                    </div>

                                    <!-- Selected Log Inspector -->
                                    <div class="md:col-span-8">
                                        {#if selectedLog}
                                            <div
                                                class="bg-slate-950 text-slate-200 rounded-2xl overflow-hidden border border-slate-800 shadow-2xl flex flex-col h-[300px]"
                                                transition:fade={{
                                                    duration: 100,
                                                }}
                                            >
                                                <div
                                                    class="px-4 py-3 bg-slate-900/50 border-b border-slate-800 flex items-center justify-between"
                                                >
                                                    <div
                                                        class="flex items-center"
                                                    >
                                                        <div
                                                            class="h-3 w-3 rounded-full bg-rose-500 mr-2"
                                                        ></div>
                                                        <div
                                                            class="h-3 w-3 rounded-full bg-amber-500 mr-2"
                                                        ></div>
                                                        <div
                                                            class="h-3 w-3 rounded-full bg-emerald-500"
                                                        ></div>
                                                        <span
                                                            class="text-[10px] font-mono text-slate-400 font-bold ml-2"
                                                            >sandbox-runner --id {selectedLog.id.slice(
                                                                0,
                                                                8,
                                                            )}</span
                                                        >
                                                    </div>
                                                    <span
                                                        class="text-[9px] font-black uppercase tracking-widest px-2 py-0.5 rounded
                                                        {selectedLog.status ===
                                                        'SUCCESS'
                                                            ? 'bg-emerald-500/10 text-emerald-400'
                                                            : 'bg-rose-500/10 text-rose-400'}"
                                                    >
                                                        {selectedLog.status} (Code
                                                        {selectedLog.exitCode})
                                                    </span>
                                                </div>
                                                <div
                                                    class="p-4 font-mono text-xs overflow-auto flex-1 space-y-4 select-text leading-relaxed"
                                                >
                                                    {#if selectedLog.stdout}
                                                        <div class="space-y-1">
                                                            <div
                                                                class="text-[10px] text-emerald-400 font-bold uppercase tracking-wider select-none"
                                                            >
                                                                stdout
                                                            </div>
                                                            <pre
                                                                class="bg-slate-900/60 p-3 rounded-xl border border-slate-900/80 whitespace-pre-wrap">{selectedLog.stdout}</pre>
                                                        </div>
                                                    {/if}
                                                    {#if selectedLog.stderr}
                                                        <div class="space-y-1">
                                                            <div
                                                                class="text-[10px] text-rose-400 font-bold uppercase tracking-wider select-none"
                                                            >
                                                                stderr
                                                            </div>
                                                            <pre
                                                                class="bg-rose-950/20 text-rose-300 p-3 rounded-xl border border-rose-900/20 whitespace-pre-wrap">{selectedLog.stderr}</pre>
                                                        </div>
                                                    {/if}
                                                    {#if !selectedLog.stdout && !selectedLog.stderr}
                                                        <p
                                                            class="text-slate-500 italic py-4 text-center select-none"
                                                        >
                                                            Execution completed
                                                            silently. No console
                                                            output generated.
                                                        </p>
                                                    {/if}
                                                </div>
                                            </div>
                                        {:else}
                                            <div
                                                class="h-full flex flex-col items-center justify-center text-center space-y-3 bg-slate-50 border border-slate-200 border-dashed rounded-2xl"
                                            >
                                                <Terminal
                                                    class="w-8 h-8 text-slate-200"
                                                />
                                                <p
                                                    class="text-[10px] font-black text-slate-400 uppercase tracking-widest"
                                                >
                                                    Select an execution entry to
                                                    inspect results
                                                </p>
                                            </div>
                                        {/if}
                                    </div>
                                </div>
                            {/if}
                        </div>
                    {/if}
                {/if}
            </div>
        </div>
    {/if}
</div>

<!-- Fullscreen Modal Overlay -->
{#if isFullscreen}
    <div
        class="fixed inset-0 z-[999] bg-slate-950 flex flex-col p-8 space-y-6"
        transition:fade
    >
        <div
            class="flex items-center justify-between border-b border-slate-800 pb-6"
        >
            <div class="space-y-1">
                <h3
                    class="text-2xl font-black text-white flex items-center gap-3 tracking-tight"
                >
                    <Cpu class="w-6 h-6 text-indigo-500" />
                    {editedPlan.name || "Untitled Plan"} (Fullscreen Coding Canvas)
                </h3>
                <p
                    class="text-[10px] font-black text-slate-500 uppercase tracking-widest ml-1"
                >
                    Secure Execution Environment • ES6 Sandbox
                </p>
            </div>

            <button
                type="button"
                onclick={() => (isFullscreen = false)}
                class="px-5 py-3 bg-slate-800 hover:bg-slate-700 text-xs font-black uppercase tracking-wider rounded-xl transition-all shadow-lg active:scale-95"
            >
                Return to Audit
            </button>
        </div>

        <div
            class="flex-1 min-h-0 bg-slate-950 rounded-3xl overflow-hidden border border-slate-800 relative shadow-2xl"
        >
            <MonacoEditor bind:value={editedPlan.code} />
        </div>

        <div class="flex items-center justify-between">
            <div class="flex items-center gap-6">
                <div class="flex items-center gap-2">
                    <div
                        class="h-2.5 w-2.5 rounded-full bg-emerald-500 animate-pulse"
                    ></div>
                    <span
                        class="text-[10px] font-black text-emerald-500/80 uppercase tracking-widest"
                        >Runtime: Online</span
                    >
                </div>
                <div class="text-[10px] font-mono text-slate-500">
                    Node v20.x Isolation Container
                </div>
            </div>

            <button
                onclick={savePlan}
                disabled={isSaving}
                class="btn-primary !bg-white !text-slate-900 !border-white flex items-center gap-2"
            >
                {#if isSaving}
                    <RefreshCw class="w-4 h-4 animate-spin text-slate-900" />
                {:else}
                    <Save class="w-4 h-4 text-slate-900" />
                {/if}
                Save & Hot-Reload
            </button>
        </div>
    </div>
{/if}

<style>
    @reference "../../app.css";

    .btn-primary {
        @apply px-6 py-3 bg-slate-900 text-white rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] hover:bg-indigo-600 hover:shadow-xl hover:shadow-indigo-200 transition-all active:scale-95 disabled:opacity-50 border border-slate-900;
    }

</style>
