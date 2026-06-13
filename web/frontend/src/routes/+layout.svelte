<script lang="ts">
    import "../app.css";
    import { auth } from "$lib/stores/auth.svelte";
    import { page } from "$app/state";
    import { goto } from "$app/navigation";
    import { onMount } from "svelte";
    import {
        Loader2,
        LogOut,
        LayoutDashboard,
        Layers,
        TrendingUp,
        Activity,
        ShieldAlert,
        Fingerprint,
        ArrowRight,
        Cpu,
        Sun,
        Moon,
        Calendar,
        ChevronDown,
        Terminal,
    } from "@lucide/svelte";
    import {
        Chart as ChartJS,
        Title,
        Tooltip,
        Legend,
        BarElement,
        CategoryScale,
        LinearScale,
        PointElement,
        LineElement,
        Filler,
    } from "chart.js";
    import { upgradeSecurityKey } from "$lib/utils/auth.svelte";
    import { fade, slide } from "svelte/transition";
    import { initWebSocketFetch } from "$lib/utils/ws_fetch";

    let { children } = $props();

    // High-level lifecycle state
    let mounted = $state(false);
    let isDark = $state(false);
    let activeDropdown = $state<string | null>(null);

    $effect(() => {
        if (mounted && !auth.isLoading) {
            const isAuthRoute = page.url.pathname.startsWith("/auth");

            if (!auth.isAuthenticated && !isAuthRoute) {
                goto("/auth/login");
            } else if (auth.isAuthenticated && isAuthRoute) {
                goto("/dashboard");
            }
        }
    });

    onMount(() => {
        initWebSocketFetch();
        mounted = true;

        // Initialize theme
        const savedTheme = localStorage.getItem("theme");
        if (savedTheme === "dark" || (!savedTheme && window.matchMedia("(prefers-color-scheme: dark)").matches)) {
            isDark = true;
            document.documentElement.classList.add("dark");
        } else {
            isDark = false;
            document.documentElement.classList.remove("dark");
        }

        try {
            ChartJS.register(
                Title,
                Tooltip,
                Legend,
                BarElement,
                CategoryScale,
                LinearScale,
                PointElement,
                LineElement,
                Filler,
            );
        } catch (e) {
            console.error("ChartJS registration failed:", e);
        }

        const handleClickOutside = (e: MouseEvent) => {
            if (activeDropdown && !(e.target as Element).closest(".nav-dropdown-container")) {
                activeDropdown = null;
            }
        };
        window.addEventListener("click", handleClickOutside);
        return () => window.removeEventListener("click", handleClickOutside);
    });

    function toggleTheme() {
        isDark = !isDark;
        if (isDark) {
            document.documentElement.classList.add("dark");
            localStorage.setItem("theme", "dark");
        } else {
            document.documentElement.classList.remove("dark");
            localStorage.setItem("theme", "light");
        }
    }

    function handleLogout() {
        auth.logout();
        goto("/auth/login");
    }

    async function handleUpgrade() {
        try {
            await upgradeSecurityKey();
            location.reload();
        } catch (err: any) {
            alert(err.message);
        }
    }

    const navItems = [
        { name: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
        { 
            name: "Planning", 
            icon: Calendar,
            children: [
                { name: "Timeline", href: "/timeline", icon: Calendar },
                { name: "Scenarios", href: "/scenarios", icon: Layers },
                { name: "Analytics", href: "/analytics", icon: TrendingUp },
            ]
        },
        { name: "Realtime", href: "/realtime", icon: Activity },
        { 
            name: "System", 
            icon: ShieldAlert,
            children: [
                { name: "Automations", href: "/automations", icon: Cpu },
                { name: "Sync Logs", href: "/sysadmin/sync-logs", icon: Terminal },
                { name: "Sysadmin", href: "/sysadmin", icon: ShieldAlert },
            ]
        },
    ];

    // Defensive accessors for template
    const showNav = $derived(
        mounted &&
            auth.isAuthenticated &&
            auth.user &&
            auth.user.scope !== "RECOVERY",
    );
    const isRecovery = $derived(
        mounted &&
            auth.isAuthenticated &&
            auth.user &&
            auth.user.scope === "RECOVERY",
    );
</script>

<div class="min-h-screen bg-slate-50 font-sans antialiased text-slate-900">
    <!-- Critical Loading Shield -->
    {#if !mounted || auth.isLoading}
        <div
            class="fixed inset-0 bg-white flex flex-col items-center justify-center space-y-4 z-[9999]"
        >
            <Loader2 class="h-10 w-10 text-indigo-600 animate-spin" />
            <p
                class="text-gray-500 font-black uppercase tracking-[0.2em] text-[10px] animate-pulse"
            >
                Initializing Wealth Engine...
            </p>
        </div>
    {/if}

    {#if mounted}
        {#if showNav}
            <nav class="glass-nav" transition:fade>
                <div class="max-w-[1600px] mx-auto px-4 sm:px-6 lg:px-8">
                    <div class="flex justify-between h-16">
                        <div class="flex">
                            <div class="flex-shrink-0 flex items-center">
                                <div
                                    class="bg-indigo-600 p-1.5 rounded-lg mr-2 shadow-lg shadow-indigo-100"
                                >
                                    <TrendingUp class="h-5 w-5 text-white" />
                                </div>
                                <span
                                    class="text-xl font-black text-slate-900 tracking-tight"
                                    >WealthEngine</span
                                >
                            </div>
                            <div
                                class="hidden sm:-my-px sm:ml-10 sm:flex sm:space-x-8"
                            >
                                {#each navItems as item}
                                    {@const Icon = item.icon}
                                    {#if item.children}
                                        <div class="relative flex nav-dropdown-container">
                                            <button
                                                onclick={(e) => {
                                                    e.stopPropagation();
                                                    activeDropdown = activeDropdown === item.name ? null : item.name;
                                                }}
                                                class="inline-flex items-center px-1 pt-1 border-b-2 text-sm font-black transition-all uppercase tracking-[0.2em] text-[11px] outline-none cursor-pointer
                                                    {item.children.some(child => page.url.pathname.startsWith(child.href))
                                                        ? 'border-indigo-600 text-slate-900'
                                                        : 'border-transparent text-slate-400 hover:text-slate-600 hover:border-slate-300'}"
                                            >
                                                <Icon class="h-4 w-4 mr-2" />
                                                {item.name}
                                                <ChevronDown class="h-3 w-3 ml-1 transition-transform {activeDropdown === item.name ? 'rotate-180' : ''}" />
                                            </button>

                                            {#if activeDropdown === item.name}
                                                <div
                                                    transition:fade={{ duration: 100 }}
                                                    class="absolute top-full left-0 mt-1 w-48 bg-white rounded-2xl shadow-xl border border-slate-100 py-2 z-50"
                                                >
                                                    {#each item.children as child}
                                                        {@const ChildIcon = child.icon}
                                                        <a
                                                            href={child.href}
                                                            onclick={() => activeDropdown = null}
                                                            class="flex items-center px-4 py-3 text-[10px] font-black uppercase tracking-[0.2em] transition-colors
                                                                {page.url.pathname.startsWith(child.href)
                                                                    ? 'text-indigo-600 bg-indigo-50/50'
                                                                    : 'text-slate-400 hover:text-slate-900 hover:bg-slate-50'}"
                                                        >
                                                            <ChildIcon class="h-3.5 w-3.5 mr-3" />
                                                            {child.name}
                                                        </a>
                                                    {/each}
                                                </div>
                                            {/if}
                                        </div>
                                    {:else}
                                        <a
                                            href={item.href}
                                            class="inline-flex items-center px-1 pt-1 border-b-2 text-sm font-black transition-all uppercase tracking-[0.2em] text-[11px]
                                                {page.url.pathname.startsWith(item.href)
                                                    ? 'border-indigo-600 text-slate-900'
                                                    : 'border-transparent text-slate-400 hover:text-slate-600 hover:border-slate-300'}"
                                        >
                                            <Icon class="h-4 w-4 mr-2" />
                                            {item.name}
                                        </a>
                                    {/if}
                                {/each}
                            </div>
                        </div>
                        <div class="flex items-center space-x-6">
                            <!-- Theme Toggle Button -->
                            <button
                                onclick={toggleTheme}
                                class="p-2 text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 rounded-xl transition-all shadow-sm border border-transparent hover:border-indigo-100"
                                title="Toggle Theme"
                            >
                                {#if isDark}
                                    <Sun class="h-5 w-5 text-amber-500" />
                                {:else}
                                    <Moon class="h-5 w-5 text-slate-400" />
                                {/if}
                            </button>

                            <a href="/profile" class="hidden md:flex flex-col items-end hover:opacity-80 transition-opacity">
                                <span
                                    class="text-xs font-black text-slate-900 tracking-tight"
                                    >{auth.user?.username}</span
                                >
                                <span
                                    class="text-[9px] text-slate-400 font-black uppercase tracking-[0.2em]"
                                    >Verified Node</span
                                >
                            </a>
                            <button
                                onclick={handleLogout}
                                class="p-2 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-xl transition-all group shadow-sm border border-transparent hover:border-red-100"
                                title="De-authenticate"
                            >
                                <LogOut class="h-5 w-5" />
                            </button>
                        </div>
                    </div>
                </div>
            </nav>
        {/if}

        <main
            class={showNav
                ? "max-w-[1600px] mx-auto py-12 px-4 sm:px-6 lg:px-8"
                : ""}
        >
            {@render children()}
        </main>

        <!-- Recovery Lockdown Overlay -->
        {#if isRecovery}
            <div
                class="fixed inset-0 bg-slate-900/80 backdrop-blur-2xl z-[1000] flex items-center justify-center p-6"
                transition:fade
            >
                <div
                    class="bg-white w-full max-w-lg rounded-[50px] shadow-2xl overflow-hidden p-12 text-center space-y-8"
                    transition:slide
                >
                    <div
                        class="inline-flex items-center justify-center p-6 bg-amber-50 rounded-[40px] shadow-sm"
                    >
                        <ShieldAlert class="h-16 w-16 text-amber-600" />
                    </div>

                    <div class="space-y-2">
                        <h2
                            class="text-4xl font-black text-slate-900 tracking-tight"
                        >
                            Access Locked
                        </h2>
                        <p class="text-slate-500 font-medium">
                            You are logged in via recovery token. You must
                            register a new hardware security key to restore full
                            access.
                        </p>
                    </div>

                    <div class="space-y-4">
                        <button
                            onclick={handleUpgrade}
                            class="btn-primary w-full py-6 text-xl flex items-center justify-center gap-4 group !bg-indigo-600 !border-indigo-600"
                        >
                            <Fingerprint class="h-6 w-6" />
                            <span>Register New Passkey</span>
                            <ArrowRight
                                class="w-5 h-5 group-hover:translate-x-1 transition-transform"
                            />
                        </button>

                        <button
                            onclick={handleLogout}
                            class="text-slate-400 font-bold hover:text-slate-600 transition-all text-sm uppercase tracking-[0.2em]"
                        >
                            Cancel and Logout
                        </button>
                    </div>
                </div>
            </div>
        {/if}
    {/if}
</div>
