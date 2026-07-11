<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        IntegrationSaveRequestSchema,
        IntegrationSchema,
        EBAspspsRequestSchema,
        EBAspspsListSchema,
        EBLinkRequestSchema,
        EBLinkResponseSchema,
        GCInstitutionsRequestSchema,
        GCInstitutionsListSchema,
        GCLinkRequestSchema,
        GCLinkResponseSchema,
    } from "$lib/gen/api_pb.js";

    import {
        Plus,
        ArrowRight,
        ArrowLeft,
        ShieldCheck,
        Banknote,
        CheckCircle2,
        Loader2,
        X,
        Search,
        Info,
        AlertCircle,
        Zap,
        Globe,
        Languages,
        CreditCard,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    let { onComplete, onCancel } = $props();

    let step = $state(0);
    let selectedType = $state<
        "GOCARDLESS" | "TRADING212" | "ENABLEBANKING" | null
    >(null);
    let isLoading = $state(false);
    let error = $state<string | null>(null);

    // Form State (Shared)
    let name = $state("");
    let syncIntervalSeconds = $state(21600); // Default 6h

    // GC Specific State
    let secretID = $state("");
    let secretKey = $state("");

    // T212 Specific State
    let t212ApiKey = $state("");
    let t212ApiSecret = $state("");

    // EB Specific State
    let ebApplicationID = $state("");
    let ebPrivateKey = $state("");
    let useExistingSession = $state(false);
    let ebExistingSessionID = $state("");

    // API State
    let integration = $state<any>(null);
    let institutions = $state<any[]>([]);
    let selectedCountry = $state("DE");
    let searchQuery = $state("");

    const countryOptions = [
        { id: "DE", label: "Germany" },
        { id: "AT", label: "Austria" },
    ];

    async function createGCIntegration() {
        isLoading = true;
        error = null;
        try {
            const [resp, err] = await wsCall(
                "integrations::save",
                IntegrationSaveRequestSchema,
                {
                    id: "",
                    name: name,
                    serviceType: "GOCARDLESS",
                    secretId: secretID,
                    secretKey: secretKey,
                    syncIntervalSeconds: syncIntervalSeconds,
                },
                [IntegrationSchema],
            ).one();
            if (err) throw err;

            integration = resp;
            step = 2;
            await fetchInstitutions();
        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    async function createT212Integration() {
        isLoading = true;
        error = null;
        try {
            const [, err] = await wsCall(
                "integrations::save",
                IntegrationSaveRequestSchema,
                {
                    id: "",
                    name: name,
                    serviceType: "TRADING212",
                    apiKey: t212ApiKey,
                    apiSecret: t212ApiSecret,
                    syncIntervalSeconds: syncIntervalSeconds,
                },
                [IntegrationSchema],
            ).one();
            if (err) throw err;

            onComplete();
        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    async function createEBIntegration() {
        isLoading = true;
        error = null;
        try {
            const [resp, err] = await wsCall(
                "integrations::save",
                IntegrationSaveRequestSchema,
                {
                    id: "",
                    name: name,
                    serviceType: "ENABLEBANKING",
                    applicationId: ebApplicationID,
                    privateKey: ebPrivateKey,
                    syncIntervalSeconds: syncIntervalSeconds,
                },
                [IntegrationSchema],
            ).one();
            if (err) throw err;

            integration = resp;
            step = 2;
            await fetchEBInstitutions();
        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    async function fetchEBInstitutions() {
        isLoading = true;
        try {
            const [resp, err] = await wsCall(
                "integrations::enablebanking::aspsps",
                EBAspspsRequestSchema,
                {
                    id: integration.integrationId,
                    country: selectedCountry,
                },
                [EBAspspsListSchema],
            ).one();
            if (err) throw err;
            institutions = resp?.aspsps ?? [];
        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    async function linkEBBank(inst: any) {
        isLoading = true;
        try {
            const state = `${integration.integrationId}:${crypto.randomUUID()}`;
            localStorage.setItem("eb_auth_state", state);

            const [resp, err] = await wsCall(
                "integrations::enablebanking::link",
                EBLinkRequestSchema,
                {
                    id: integration.integrationId,
                    bankName: inst.name,
                    country: inst.country,
                    redirectUrl:
                        "https://budget.genazt.me/auth/oauth2/callback",
                    state: state,
                },
                [EBLinkResponseSchema],
            ).one();
            if (err) throw err;

            if (resp) {
                window.location.href = resp.url;
            }
        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    async function fetchInstitutions() {
        isLoading = true;
        try {
            const [resp, err] = await wsCall(
                "integrations::gocardless::institutions",
                GCInstitutionsRequestSchema,
                {
                    id: integration.integrationId,
                    country: selectedCountry,
                },
                [GCInstitutionsListSchema],
            ).one();
            if (err) throw err;
            institutions = resp?.institutions ?? [];
        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    async function linkBank(inst: any) {
        isLoading = true;
        try {
            const [resp, err] = await wsCall(
                "integrations::gocardless::link",
                GCLinkRequestSchema,
                {
                    id: integration.integrationId,
                    institutionId: inst.id,
                    redirectUrl: window.location.href,
                },
                [GCLinkResponseSchema],
            ).one();
            if (err) throw err;
            if (resp) {
                window.location.href = resp.link;
            }
        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    const filteredInstitutions = $derived(
        institutions.filter((i) =>
            i.name.toLowerCase().includes(searchQuery.toLowerCase()),
        ),
    );
</script>

<div
    class="fixed inset-0 bg-slate-900/60 z-[100] flex items-center justify-center p-6"
    transition:fade
>
    <div
        class="bg-white w-full max-w-3xl rounded-[30px] shadow-2xl overflow-hidden flex flex-col max-h-[90vh] relative"
        transition:slide
    >
        <div
            class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
        ></div>

        <!-- Header -->
        <div
            class="p-10 border-b border-slate-50 flex items-center justify-between"
        >
            <div class="flex items-center gap-6">
                <div
                    class="w-16 h-16 bg-slate-50 rounded-3xl border border-slate-100 flex items-center justify-center"
                >
                    {#if step === 0}<ShieldCheck
                            class="w-8 h-8 text-indigo-600"
                        />
                    {:else if selectedType === "GOCARDLESS"}<Banknote
                            class="w-8 h-8 text-indigo-600"
                        />
                    {:else}<Zap class="w-8 h-8 text-indigo-600" />{/if}
                </div>
                <div>
                    <h3
                        class="text-2xl font-black tracking-tight text-slate-900"
                    >
                        Integration Wizard
                    </h3>
                    <p class="text-slate-500 font-medium text-sm">
                        Establish a secure, identity-bound data chain.
                    </p>
                </div>
            </div>
            <button
                onclick={onCancel}
                class="p-4 hover:bg-slate-50 rounded-2xl transition-all border border-transparent hover:border-slate-100"
            >
                <X class="w-6 h-6 text-slate-400" />
            </button>
        </div>

        <!-- Progress Bar -->
        <div class="h-1 w-full bg-slate-100">
            <div
                class="h-full bg-indigo-600 transition-all duration-500"
                style="width: {(step + 1) * 33.33}%"
            ></div>
        </div>

        <div class="p-10 flex-1 overflow-y-auto space-y-10 custom-scrollbar">
            {#if error}
                <div
                    class="p-6 bg-rose-50 text-rose-700 rounded-2xl border border-rose-100 flex gap-4 animate-in shake-1 shadow-sm shadow-rose-100"
                    transition:slide
                >
                    <AlertCircle class="w-6 h-6 flex-shrink-0" />
                    <p class="font-bold">{error}</p>
                </div>
            {/if}

            {#if step === 0}
                <div class="space-y-8" in:fade>
                    <div>
                        <h3
                            class="text-xl font-black text-slate-900 tracking-tight"
                        >
                            Choose Provider
                        </h3>
                        <p class="text-slate-500 font-medium text-sm">
                            Select the pipeline type you wish to establish.
                        </p>
                    </div>

                    <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
                        <button
                            onclick={() => {
                                selectedType = "GOCARDLESS";
                                step = 1;
                            }}
                            class="p-8 bg-white rounded-[32px] border border-slate-100 shadow-sm hover:border-indigo-600 hover:shadow-xl hover:shadow-indigo-100/50 transition-all group text-left flex flex-col gap-4"
                        >
                            <div
                                class="w-14 h-14 bg-slate-50 rounded-2xl flex items-center justify-center group-hover:bg-indigo-50 transition-colors"
                            >
                                <Banknote
                                    class="w-7 h-7 text-slate-400 group-hover:text-indigo-600 transition-colors"
                                />
                            </div>
                            <div>
                                <h4 class="font-black text-slate-900 text-lg">
                                    GoCardless
                                </h4>
                                <p
                                    class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                                >
                                    European PSD2
                                </p>
                            </div>
                        </button>

                        <button
                            onclick={() => {
                                selectedType = "TRADING212";
                                step = 1;
                            }}
                            class="p-8 bg-white rounded-[32px] border border-slate-100 shadow-sm hover:border-indigo-600 hover:shadow-xl hover:shadow-indigo-100/50 transition-all group text-left flex flex-col gap-4"
                        >
                            <div
                                class="w-14 h-14 bg-slate-50 rounded-2xl flex items-center justify-center group-hover:bg-indigo-50 transition-colors"
                            >
                                <Zap
                                    class="w-7 h-7 text-slate-400 group-hover:text-indigo-600 transition-colors"
                                />
                            </div>
                            <div>
                                <h4 class="font-black text-slate-900 text-lg">
                                    Trading 212
                                </h4>
                                <p
                                    class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                                >
                                    Official API
                                </p>
                            </div>
                        </button>

                        <button
                            onclick={() => {
                                selectedType = "ENABLEBANKING";
                                step = 1;
                            }}
                            class="p-8 bg-white rounded-[32px] border border-slate-100 shadow-sm hover:border-indigo-600 hover:shadow-xl hover:shadow-indigo-100/50 transition-all group text-left flex flex-col gap-4"
                        >
                            <div
                                class="w-14 h-14 bg-slate-50 rounded-2xl flex items-center justify-center group-hover:bg-indigo-50 transition-colors"
                            >
                                <Globe
                                    class="w-7 h-7 text-slate-400 group-hover:text-indigo-600 transition-colors"
                                />
                            </div>
                            <div>
                                <h4 class="font-black text-slate-900 text-lg">
                                    Enable Banking
                                </h4>
                                <p
                                    class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                                >
                                    Global Open Banking
                                </p>
                            </div>
                        </button>
                    </div>
                </div>
            {:else if step === 1}
                <div class="space-y-8" in:fade>
                    <div class="grid grid-cols-1 gap-8">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Instance Name</label
                            >
                            <input
                                type="text"
                                bind:value={name}
                                placeholder={selectedType === "GOCARDLESS"
                                    ? "e.g. Personal N26"
                                    : "e.g. T212 Portfolio"}
                                class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-bold outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                            />
                        </div>

                        {#if selectedType === "GOCARDLESS"}
                            <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                <div class="space-y-2">
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >GoCardless Secret ID</label
                                    >
                                    <input
                                        type="password"
                                        bind:value={secretID}
                                        placeholder="UUID format"
                                        class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-mono text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                    />
                                </div>
                                <div class="space-y-2">
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >GoCardless Secret Key</label
                                    >
                                    <input
                                        type="password"
                                        bind:value={secretKey}
                                        placeholder="Long hex string"
                                        class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-mono text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                    />
                                </div>
                            </div>
                        {:else if selectedType === "ENABLEBANKING"}
                            <div class="space-y-6">
                                <div class="space-y-2">
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >Enable Banking Application ID</label
                                    >
                                    <input
                                        type="text"
                                        bind:value={ebApplicationID}
                                        placeholder="Application ID"
                                        class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-bold outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                    />
                                </div>
                                <div class="space-y-2">
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >RSA Private Key (PEM)</label
                                    >
                                    <textarea
                                        bind:value={ebPrivateKey}
                                        placeholder="-----BEGIN RSA PRIVATE KEY-----..."
                                        rows="6"
                                        class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-mono text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                    ></textarea>
                                </div>
                                <div
                                    class="flex items-center justify-between p-4 bg-slate-50 border border-slate-100 rounded-2xl"
                                >
                                    <div class="space-y-1">
                                        <h4
                                            class="text-xs font-bold text-slate-800"
                                        >
                                            Use Existing Session
                                        </h4>
                                        <p
                                            class="text-[10px] text-slate-500 font-medium"
                                        >
                                            Link an already authorized Enable
                                            Banking session ID.
                                        </p>
                                    </div>
                                    <label
                                        class="relative inline-flex items-center cursor-pointer"
                                    >
                                        <input
                                            type="checkbox"
                                            bind:checked={useExistingSession}
                                            class="sr-only peer"
                                        />
                                        <div
                                            class="w-11 h-6 bg-slate-200 peer-focus:outline-none rounded-full peer peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-slate-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-indigo-600"
                                        ></div>
                                    </label>
                                </div>

                                {#if useExistingSession}
                                    <div class="space-y-2" transition:slide>
                                        <label
                                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                            >Existing Session ID</label
                                        >
                                        <input
                                            type="text"
                                            bind:value={ebExistingSessionID}
                                            placeholder="UUID format"
                                            class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-mono text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                        />
                                    </div>
                                {/if}
                            </div>
                        {:else if selectedType === "TRADING212"}
                            <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                <div class="space-y-2">
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >T212 API Key</label
                                    >
                                    <input
                                        type="password"
                                        bind:value={t212ApiKey}
                                        placeholder="API Key"
                                        class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-mono text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                    />
                                </div>
                                <div class="space-y-2">
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >T212 API Secret</label
                                    >
                                    <input
                                        type="password"
                                        bind:value={t212ApiSecret}
                                        placeholder="API Secret"
                                        class="w-full px-6 py-4 bg-white border border-slate-200 rounded-2xl font-mono text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                    />
                                </div>
                            </div>
                            <p
                                class="text-[9px] font-medium text-slate-400 px-1 leading-relaxed"
                            >
                                Generate these in Settings → API (Beta).
                                Requires <strong>Read-only</strong> permissions for
                                sync.
                            </p>
                        {/if}

                        <div class="space-y-4">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Sync Frequency</label
                            >
                            <div class="grid grid-cols-3 gap-3">
                                {#each [{ label: "High (6h)", val: 21600 }, { label: "Standard (12h)", val: 43200 }, { label: "Eco (24h)", val: 86400 }] as opt}
                                    <button
                                        onclick={() =>
                                            (syncIntervalSeconds = opt.val)}
                                        class="py-3 px-4 rounded-xl border-2 font-black text-[10px] uppercase tracking-[0.2em] transition-all {syncIntervalSeconds ===
                                        opt.val
                                            ? 'bg-indigo-600 border-indigo-600 text-white shadow-lg shadow-indigo-100'
                                            : 'bg-white border-slate-100 text-slate-400 hover:border-slate-200'}"
                                        >{opt.label}</button
                                    >
                                {/each}
                            </div>
                        </div>
                    </div>

                    <div
                        class="p-6 bg-amber-50 rounded-3xl border border-amber-100 flex gap-4 shadow-sm shadow-amber-100"
                    >
                        <Info class="w-6 h-6 text-amber-500 flex-shrink-0" />
                        <p
                            class="text-xs leading-relaxed text-amber-800 font-medium"
                        >
                            Your credentials will be encrypted using a key
                            derived from your <strong
                                >Hardware Authenticator</strong
                            >
                            and this <strong>Server Instance</strong>.
                        </p>
                    </div>

                    <div class="flex gap-4 pt-4">
                        <button
                            onclick={() => (step = 0)}
                            class="p-5 bg-slate-50 text-slate-400 rounded-2xl hover:bg-slate-100 hover:text-slate-600 transition-all border border-transparent hover:border-slate-200"
                            ><ArrowLeft class="w-6 h-6" /></button
                        >
                        <button
                            onclick={selectedType === "GOCARDLESS"
                                ? createGCIntegration
                                : selectedType === "ENABLEBANKING"
                                  ? createEBIntegration
                                  : createT212Integration}
                            disabled={isLoading ||
                                !name ||
                                (selectedType === "GOCARDLESS"
                                    ? !secretID || !secretKey
                                    : selectedType === "ENABLEBANKING"
                                      ? !ebApplicationID ||
                                        !ebPrivateKey ||
                                        (useExistingSession &&
                                            !ebExistingSessionID)
                                      : !t212ApiKey || !t212ApiSecret)}
                            class="btn-primary flex-1 py-5 text-lg group bg-indigo-600 hover:bg-indigo-700 shadow-indigo-200"
                        >
                            {#if isLoading}<Loader2
                                    class="w-6 h-6 animate-spin"
                                /><span>Initializing...</span>
                            {:else}<span>Initialize Data Chain</span><ArrowRight
                                    class="w-5 h-5 group-hover:translate-x-1 transition-transform"
                                />{/if}
                        </button>
                    </div>
                </div>
            {:else if step === 2}
                {#if selectedType === "GOCARDLESS" || selectedType === "ENABLEBANKING"}
                    <div class="space-y-8" in:fade>
                        <div
                            class="flex flex-col md:flex-row md:items-center justify-between gap-6 border-b border-slate-50 pb-10"
                        >
                            <div>
                                <h3
                                    class="text-2xl font-black tracking-tight text-slate-900"
                                >
                                    Select Institution
                                </h3>
                                <p class="text-slate-500 font-medium text-sm">
                                    Select your provider to authorize the OAuth
                                    bridge.
                                </p>
                            </div>
                            <div class="flex items-center gap-3">
                                <div class="w-32">
                                    <SearchableDropdown
                                        options={countryOptions}
                                        bind:value={selectedCountry}
                                        onchange={selectedType === "GOCARDLESS"
                                            ? fetchInstitutions
                                            : fetchEBInstitutions}
                                    />
                                </div>
                                <div class="relative flex-1">
                                    <Search
                                        class="w-4 h-4 text-slate-400 absolute left-3.5 top-1/2 -translate-y-1/2"
                                    />
                                    <input
                                        type="text"
                                        bind:value={searchQuery}
                                        placeholder="Search banks..."
                                        class="w-full pl-10 pr-4 py-3 bg-white border border-slate-200 rounded-xl font-bold text-sm outline-none focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                                    />
                                </div>
                            </div>
                        </div>
                        {#if isLoading}
                            <div
                                class="p-20 flex flex-col items-center justify-center space-y-4"
                            >
                                <Loader2
                                    class="w-12 h-12 text-indigo-600 animate-spin"
                                />
                                <p
                                    class="text-xs font-black uppercase tracking-[0.2em] text-slate-400"
                                >
                                    Loading Banks...
                                </p>
                            </div>
                        {:else}
                            <div class="grid grid-cols-2 md:grid-cols-4 gap-6">
                                {#each filteredInstitutions as inst}
                                    <button
                                        onclick={() =>
                                            selectedType === "GOCARDLESS"
                                                ? linkBank(inst)
                                                : linkEBBank(inst)}
                                        class="p-6 bg-white rounded-3xl border border-slate-100 shadow-sm hover:border-indigo-600 hover:shadow-xl hover:shadow-indigo-100/30 transition-all flex flex-col items-center gap-4 group"
                                    >
                                        {#if inst.logo}
                                            <img
                                                src={inst.logo}
                                                alt={inst.name}
                                                class="w-16 h-16 rounded-2xl bg-white p-2 border border-slate-50 shadow-sm transition-transform group-hover:scale-110"
                                            />
                                        {:else}
                                            <div
                                                class="w-16 h-16 rounded-2xl bg-slate-50 flex items-center justify-center transition-transform group-hover:scale-110"
                                            >
                                                <Banknote
                                                    class="w-8 h-8 text-slate-400"
                                                />
                                            </div>
                                        {/if}
                                        <span
                                            class="text-[10px] font-black text-center uppercase text-slate-600 tracking-tight"
                                            >{inst.name}</span
                                        >
                                    </button>
                                {/each}
                            </div>
                        {/if}
                    </div>
                {/if}
            {/if}
        </div>
    </div>
</div>

<style>
    .custom-scrollbar::-webkit-scrollbar {
        width: 4px;
    }
    .custom-scrollbar::-webkit-scrollbar-track {
        background: #f1f5f9;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #e2e8f0;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: #cbd5e1;
    }
</style>
